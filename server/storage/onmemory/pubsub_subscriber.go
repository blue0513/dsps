package onmemory

import (
	"context"

	"github.com/saiya/dsps/server/domain"
)

type onmemoryChannel struct {
	*domain.Channel
	channelClock uint64

	subscribers map[domain.SubscriberID]*onmemorySubscriber
	log         map[domain.MessageLocator]*onmemoryMessage
}

type onmemorySubscriber struct {
	lastActivity domain.Time
	channelClock uint64
	messages     []*onmemoryMessage
}

func (s *onmemoryStorage) NewSubscriber(ctx context.Context, sl domain.SubscriberLocator) error {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	ch := s.getChannel(sl.ChannelID)
	if ch == nil {
		return domain.ErrInvalidChannel
	}

	if ch.subscribers[sl.SubscriberID] != nil {
		return nil // Already exists (success)
	}

	instance := onmemorySubscriber{
		channelClock: ch.channelClock,
		lastActivity: s.systemClock.Now(),
		messages:     []*onmemoryMessage{},
	}
	ch.subscribers[sl.SubscriberID] = &instance
	return nil
}

func (s *onmemoryStorage) RemoveSubscriber(ctx context.Context, sl domain.SubscriberLocator) error {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	ch := s.getChannel(sl.ChannelID)
	if ch == nil {
		// Because channel does not exist, subscriber also does not exist.
		// This method returns nil (success) if subscriber does not exist.
		return nil
	}

	delete(ch.subscribers, sl.SubscriberID)
	return nil
}

func (s *onmemoryStorage) getChannel(id domain.ChannelID) *onmemoryChannel {
	chPtr := s.channels[id]
	if chPtr == nil {
		rawCh := s.channelProvider(id)
		if rawCh != nil {
			ch := onmemoryChannel{
				Channel:      rawCh,
				channelClock: 0,

				subscribers: map[domain.SubscriberID]*onmemorySubscriber{},
				log:         map[domain.MessageLocator]*onmemoryMessage{},
			}
			chPtr = &ch
			s.channels[id] = chPtr
		}
	}
	return chPtr
}

// Note: this method holds lock of the storage!!
func (s *onmemoryStorage) findSubscriberForFetchMessages(ctx context.Context, sl domain.SubscriberLocator) (*onmemorySubscriber, error) {
	// This method is called from FetchMessages function.
	// It does not want to lock storage during polling.
	// So that lock storage in this method instead of the FetchMessages.
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
	return sbsc, nil
}

func (sbsc *onmemorySubscriber) addMessage(msg onmemoryMessage) {
	sbsc.messages = append(sbsc.messages, &msg)
}
