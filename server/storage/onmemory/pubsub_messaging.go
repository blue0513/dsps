package onmemory

import (
	"context"
	"time"

	"github.com/saiya/dsps/server/domain"
	storageinternal "github.com/saiya/dsps/server/storage/internal"
)

type onmemoryMessage struct {
	domain.Message
	channelClock uint64
	ExpireAt     domain.Time
}

func (s *onmemoryStorage) PublishMessages(ctx context.Context, msgs []domain.Message) error {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	for _, msg := range msgs {
		ch := s.getChannel(msg.ChannelID)
		if ch == nil {
			return domain.ErrInvalidChannel
		}
		if ch.log[msg.MessageLocator] != nil {
			continue // Duplicated message
		}

		ch.channelClock = ch.channelClock + 1 // Must start with 1
		wrapped := onmemoryMessage{
			channelClock: ch.channelClock,
			ExpireAt:     domain.Time{Time: s.systemClock.Now().Add(ch.Expire.Duration)},
			Message:      msg,
		}
		ch.log[msg.MessageLocator] = &wrapped

		for _, sbsc := range ch.subscribers {
			sbsc.addMessage(wrapped)
			sbsc.lastActivity = s.systemClock.Now()
		}
	}
	return nil
}

func (s *onmemoryStorage) FetchMessages(ctx context.Context, sl domain.SubscriberLocator, max int, waituntil domain.Duration) ([]domain.Message, domain.AckHandle, error) {
	sbsc, err := s.findSubscriberForFetchMessages(ctx, sl)
	if err != nil {
		return []domain.Message{}, domain.AckHandle{}, err
	}

	endPolling := make(chan bool, 1)
	received := make(chan domain.Message, max)
	completed := make(chan error, 2)

	go func() {
		defer close(received)
		defer func() { completed <- nil }()

		pollingInterval := time.NewTicker(500 * time.Millisecond)
		defer pollingInterval.Stop()

		found := false
		full := false
	P:
		for {
			func() {
				unlock, err := s.lock.Lock(ctx)
				if err != nil {
					completed <- err
					return
				}
				defer unlock()

				sbsc.lastActivity = s.systemClock.Now()
				for _, msg := range sbsc.messages {
					select {
					case received <- msg.Message: // Receive message
						found = true
					default: // Queue is full (reached to max)
						full = true
					}
				}
			}()

			// If message(s) found, return them immediately.
			if found || full {
				break P
			}
			select {
			case <-endPolling:
				break P
			case <-pollingInterval.C:
				continue P
			}
		}
	}()

	timeoutTimer := time.NewTimer(waituntil.Duration)
	select {
	case err = <-completed: // Completed before timeout
		if err != nil {
			return []domain.Message{}, domain.AckHandle{}, err
		}
	case <-timeoutTimer.C:
		endPolling <- true
		if err := <-completed; err != nil {
			return []domain.Message{}, domain.AckHandle{}, err
		}
	}

	messages := []domain.Message{}
	for msg := range received {
		messages = append(messages, msg)
	}

	var rh domain.AckHandle
	if len(messages) > 0 {
		rh = storageinternal.EncodeAckHandle(sl, storageinternal.AckHandleData{
			LastMessageID: messages[len(messages)-1].MessageID,
		})
	}
	return messages, rh, nil
}

func (s *onmemoryStorage) AcknowledgeMessages(ctx context.Context, handle domain.AckHandle) error {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	ch := s.getChannel(handle.ChannelID)
	if ch == nil {
		return domain.ErrInvalidChannel
	}

	sbsc := ch.subscribers[handle.SubscriberID]
	if sbsc == nil {
		return domain.ErrSubscriberNotFound
	}
	sbsc.lastActivity = s.systemClock.Now()

	rhd, err := storageinternal.DecodeAckHandle(handle)
	if err != nil {
		return err
	}

	var readUntil = -1
	for i, msg := range sbsc.messages {
		if rhd.LastMessageID == msg.MessageID {
			readUntil = i
			break
		}
	}
	if readUntil == -1 {
		return nil // ReceiptHandle is stale, may be already consumed
	}
	sbsc.channelClock = sbsc.messages[readUntil].channelClock
	sbsc.messages = sbsc.messages[readUntil+1:]
	return nil
}

func (s *onmemoryStorage) IsOldMessages(ctx context.Context, sl domain.SubscriberLocator, msgs []domain.MessageLocator) (map[domain.MessageLocator]bool, error) {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	ch := s.getChannel(sl.ChannelID)
	if ch == nil {
		return nil, domain.ErrInvalidChannel
	}

	sbsc := ch.subscribers[sl.SubscriberID]
	if sbsc == nil {
		return nil, domain.ErrSubscriberNotFound
	}

	result := map[domain.MessageLocator]bool{}
	for _, msg := range msgs {
		wrapped := ch.log[msg]
		if wrapped != nil && wrapped.channelClock <= sbsc.channelClock {
			result[msg] = true
		} else {
			result[msg] = false
		}
	}
	return result, nil
}
