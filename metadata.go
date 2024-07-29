package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

type ProfileMetadata struct {
	PubKey string       `json:"-"` // must always be set otherwise things will break
	Event  *nostr.Event `json:"-"` // may be empty if a profile metadata event wasn't found

	// every one of these may be empty
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	About       string `json:"about,omitempty"`
	Website     string `json:"website,omitempty"`
	Picture     string `json:"picture,omitempty"`
	Banner      string `json:"banner,omitempty"`
	NIP05       string `json:"nip05,omitempty"`
	LUD16       string `json:"lud16,omitempty"`
}

func (p ProfileMetadata) Npub() string {
	v, _ := nip19.EncodePublicKey(p.PubKey)
	return v
}

func (p ProfileMetadata) NpubShort() string {
	npub := p.Npub()
	return npub[0:7] + "…" + npub[58:]
}

func (p ProfileMetadata) Nprofile(ctx context.Context, sys *System, nrelays int) string {
	v, _ := nip19.EncodeProfile(p.PubKey, sys.FetchOutboxRelays(ctx, p.PubKey, 2))
	return v
}

func (p ProfileMetadata) ShortName() string {
	if p.Name != "" {
		return p.Name
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.NpubShort()
}

// FetchProfileMetadata fetches metadata for a given user from the local cache, or from the local store,
// or, failing these, from the target user's defined outbox relays -- then caches the result.
func (sys *System) FetchProfileMetadata(ctx context.Context, pubkey string) ProfileMetadata {
	pm, _ := sys.fetchProfileMetadata(ctx, pubkey)
	return pm
}

// FetchOrStoreProfileMetadata is like FetchProfileMetadata, but also saves the result to the sys.Store
func (sys *System) FetchOrStoreProfileMetadata(ctx context.Context, pubkey string) ProfileMetadata {
	pm, fromInternal := sys.fetchProfileMetadata(ctx, pubkey)
	if !fromInternal && pm.Event != nil {
		sys.StoreRelay.Publish(ctx, *pm.Event)
	}
	return pm
}

func (sys *System) fetchProfileMetadata(ctx context.Context, pubkey string) (pm ProfileMetadata, fromInternal bool) {
	if pm, loaded := sys.LoadProfileMetadataFromCache(ctx, pubkey); loaded {
		return pm, true
	}

	pm.PubKey = pubkey

	thunk0 := sys.replaceableLoaders[0].Load(ctx, pubkey)
	evt, err := thunk0()
	if err == nil {
		pm, err = ParseMetadata(evt)
		if err == nil {
			sys.MetadataCache.SetWithTTL(pubkey, pm, time.Hour*6)
		}
	}
	return pm, false
}

func (sys *System) LoadProfileMetadataFromCache(ctx context.Context, pubkey string) (ProfileMetadata, bool) {
	if v, ok := sys.MetadataCache.Get(pubkey); ok {
		return v, true
	}

	if sys.Store != nil {
		res, _ := sys.StoreRelay.QuerySync(ctx, nostr.Filter{Kinds: []int{0}, Authors: []string{pubkey}})
		if len(res) != 0 {
			if m, err := ParseMetadata(res[0]); err == nil {
				m.PubKey = pubkey
				m.Event = res[0]
				sys.MetadataCache.SetWithTTL(pubkey, m, time.Hour*6)
				return m, true
			}
		}
	}

	return ProfileMetadata{PubKey: pubkey}, false
}

// FetchUserEvents fetches events from each users' outbox relays, grouping queries when possible.
func (sys *System) FetchUserEvents(ctx context.Context, filter nostr.Filter) (map[string][]*nostr.Event, error) {
	filters, err := sys.ExpandQueriesByAuthorAndRelays(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to expand queries: %w", err)
	}

	results := make(map[string][]*nostr.Event)
	wg := sync.WaitGroup{}
	wg.Add(len(filters))
	for relayURL, filter := range filters {
		go func(relayURL string, filter nostr.Filter) {
			defer wg.Done()
			filter.Limit = filter.Limit * len(filter.Authors) // hack
			for ie := range sys.Pool.SubManyEose(ctx, []string{relayURL}, nostr.Filters{filter}) {
				results[ie.PubKey] = append(results[ie.PubKey], ie.Event)
			}
		}(relayURL, filter)
	}
	wg.Wait()

	return results, nil
}

func ParseMetadata(event *nostr.Event) (meta ProfileMetadata, err error) {
	if event.Kind != 0 {
		err = fmt.Errorf("event %s is kind %d, not 0", event.ID, event.Kind)
	} else if er := json.Unmarshal([]byte(event.Content), &meta); er != nil {
		cont := event.Content
		if len(cont) > 100 {
			cont = cont[0:99]
		}
		err = fmt.Errorf("failed to parse metadata (%s) from event %s: %w", cont, event.ID, er)
	}

	meta.PubKey = event.PubKey
	meta.Event = event
	return meta, err
}
