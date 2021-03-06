package onmemory

import (
	"context"
	"fmt"
	"time"
)

var gcTimeout = 3 * time.Second

func (s *onmemoryStorage) startGC() {
	go func() {
	L:
		for {
			select {
			case <-s.gcTickerShutdownRequest:
				break L
			case <-s.gcTicker.C:
				func() {
					ctx, cancel := context.WithTimeout(context.Background(), gcTimeout)
					defer cancel()

					err := s.GC(ctx)
					if err != nil {
						fmt.Printf("Onmemory storage GC failed: %v\n", err) // TODO: Use logger instead
					}
				}()
			}
		}
	}()
}

func (s *onmemoryStorage) GC(ctx context.Context) error {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	startAt := s.systemClock.Now()
	s.stat.GC.LastStartAt = startAt
	for _, ch := range s.channels {
		if err := ctx.Err(); err != nil {
			return err // Context canceled
		}
		expireBefore := s.systemClock.Now().Add(-ch.Expire.Duration)

		for sid, sbsc := range ch.subscribers {
			if err := ctx.Err(); err != nil {
				return err // Context canceled
			}

			// Remove expired subscriber.
			if sbsc.lastActivity.Before(expireBefore) {
				delete(ch.subscribers, sid)
				s.stat.GC.Evicted.Subscribers++
				continue
			}

			// Remove expired messages from subscriber queue.
			aliveMsgs := make([]*onmemoryMessage, 0, len(sbsc.messages))
			for _, msg := range sbsc.messages {
				if !msg.ExpireAt.Before(expireBefore) {
					aliveMsgs = append(aliveMsgs, msg)
				}
			}
			sbsc.messages = aliveMsgs
		}

		// Remove expired message log.
		for msgLoc, msg := range ch.log {
			if err := ctx.Err(); err != nil {
				return err // Context canceled
			}

			if msg.ExpireAt.Before(expireBefore) {
				delete(ch.log, msgLoc)
				s.stat.GC.Evicted.Messages++
			}
		}
	}

	// Delete expired JWT revocation memory
	for jti, exp := range s.revokedJwts {
		if err := ctx.Err(); err != nil {
			return err // Context canceled
		}

		if time.Time(exp).Before(s.systemClock.Now().Time) {
			delete(s.revokedJwts, jti)
			s.stat.GC.Evicted.JwtRevocations++
		}
	}
	s.stat.GC.LastGCSec = s.systemClock.Now().Sub(startAt.Time).Seconds()

	return nil
}
