package sdk

import (
	"context"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/nostr-sdk/cache"
)

type System struct {
	RelaysCache      cache.Cache32[[]Relay]
	FollowsCache     cache.Cache32[[]Follow]
	MetadataCache    cache.Cache32[ProfileMetadata]
	Pool             *nostr.SimplePool
	RelayListRelays  []string
	FollowListRelays []string
	MetadataRelays   []string
	Store            eventstore.Store
}

func (sys System) StoreRelay() eventstore.RelayInterface {
	return eventstore.RelayWrapper{Store: sys.Store}
}

func (sys System) FetchRelays(ctx context.Context, pubkey string) []Relay {
	if v, ok := sys.RelaysCache.Get(pubkey); ok {
		return v
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	res := FetchRelaysForPubkey(ctx, sys.Pool, pubkey, sys.RelayListRelays...)
	sys.RelaysCache.SetWithTTL(pubkey, res, time.Hour*6)
	return res
}

func (sys System) FetchOutboxRelays(ctx context.Context, pubkey string) []string {
	relays := sys.FetchRelays(ctx, pubkey)
	result := make([]string, 0, len(relays))
	for _, relay := range relays {
		if relay.Outbox {
			result = append(result, relay.URL)
		}
	}
	return result
}

func (sys System) FetchProfileMetadata(ctx context.Context, pubkey string) ProfileMetadata {
	if v, ok := sys.MetadataCache.Get(pubkey); ok {
		return v
	}

	if sys.Store != nil {
		res, _ := sys.StoreRelay().QuerySync(ctx, nostr.Filter{Kinds: []int{0}, Authors: []string{pubkey}})
		if len(res) != 0 {
			if m, err := ParseMetadata(res[0]); err == nil {
				m.PubKey = pubkey
				m.Event = res[0]
				sys.MetadataCache.SetWithTTL(pubkey, *m, time.Hour*6)
				return *m
			}
		}
	}

	ctxRelays, cancel := context.WithTimeout(ctx, time.Second*2)
	relays := sys.FetchOutboxRelays(ctxRelays, pubkey)
	cancel()

	ctx, cancel = context.WithTimeout(ctx, time.Second*3)
	res := FetchProfileMetadata(ctx, sys.Pool, pubkey, append(relays, sys.MetadataRelays...)...)
	cancel()

	sys.MetadataCache.SetWithTTL(pubkey, res, time.Hour*6)
	return res
}
