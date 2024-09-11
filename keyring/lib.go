package keyring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip05"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/nbd-wtf/go-nostr/nip46"
	"github.com/nbd-wtf/go-nostr/nip49"
)

type Keyring interface {
	Signer
	Cipher
}

// A Signer provides basic public key signing methods.
type Signer interface {
	GetPublicKey(context.Context) string
	SignEvent(context.Context, *nostr.Event) error
}

// A Cipher provides NIP-44 encryption and decryption methods.
type Cipher interface {
	Encrypt(ctx context.Context, plaintext string, recipientPublicKey string) (base64ciphertext string, err error)
	Decrypt(ctx context.Context, base64ciphertext string, senderPublicKey string) (plaintext string, err error)
}

type SignerOptions struct {
	BunkerClientSecretKey string
	BunkerSignTimeout     time.Duration
	BunkerAuthHandler     func(string)

	// if a PasswordHandler is provided the key will be stored encrypted and this function will be called
	// every time an operation needs access to the key so the user can be prompted.
	PasswordHandler func(context.Context) string

	// if instead a Password is provided along with a ncryptsec, then the key will be decrypted and stored in plaintext.
	Password string
}

func New(ctx context.Context, pool *nostr.SimplePool, input string, opts *SignerOptions) (Keyring, error) {
	if opts == nil {
		opts = &SignerOptions{}
	}

	if strings.HasPrefix(input, "ncryptsec") {
		if opts.PasswordHandler != nil {
			return &EncryptedKeySigner{input, "", opts.PasswordHandler}, nil
		}
		sec, err := nip49.Decrypt(input, opts.Password)
		if err != nil {
			if opts.Password == "" {
				return nil, fmt.Errorf("failed to decrypt with blank password: %w", err)
			}
			return nil, fmt.Errorf("failed to decrypt with given password: %w", err)
		}
		pk, _ := nostr.GetPublicKey(sec)
		return KeySigner{sec, pk, make(map[string][32]byte)}, nil
	} else if nip46.IsValidBunkerURL(input) || nip05.IsValidIdentifier(input) {
		bcsk := nostr.GeneratePrivateKey()
		oa := func(url string) { println("auth_url received but not handled") }

		if opts.BunkerClientSecretKey != "" {
			bcsk = opts.BunkerClientSecretKey
		}
		if opts.BunkerAuthHandler != nil {
			oa = opts.BunkerAuthHandler
		}

		bunker, err := nip46.ConnectBunker(ctx, bcsk, input, pool, oa)
		if err != nil {
			return nil, err
		}
		return BunkerSigner{bunker}, nil
	} else if prefix, parsed, err := nip19.Decode(input); err == nil && prefix == "nsec" {
		sec := parsed.(string)
		pk, _ := nostr.GetPublicKey(sec)
		return KeySigner{sec, pk, make(map[string][32]byte)}, nil
	} else if nostr.IsValid32ByteHex(input) {
		pk, _ := nostr.GetPublicKey(input)
		return KeySigner{input, pk, make(map[string][32]byte)}, nil
	}

	return nil, fmt.Errorf("unsupported input '%s'", input)
}
