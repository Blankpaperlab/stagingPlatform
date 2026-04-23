package session_salt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"stagehand/internal/store"
)

const (
	SaltSize      = 32
	MasterKeySize = 32
	nonceSize     = 12
	envelopeV1    = "v1"
)

var emailPattern = regexp.MustCompile(`(?i)^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

var sessionCreationLocks keyedMutexMap

type Manager struct {
	store     store.ArtifactStore
	masterKey []byte
	random    io.Reader
	now       func() time.Time
}

type Material struct {
	SessionName string
	SaltID      string
	Salt        []byte
	CreatedAt   time.Time
}

type encryptedEnvelope struct {
	Version    string `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type keyedMutexMap struct {
	mu    sync.Mutex
	locks map[string]*keyedMutexRef
}

type keyedMutexRef struct {
	mu   sync.Mutex
	refs int
}

func NewManager(artifactStore store.ArtifactStore, masterKey []byte) (*Manager, error) {
	if artifactStore == nil {
		return nil, fmt.Errorf("artifact store is required")
	}

	if len(masterKey) != MasterKeySize {
		return nil, fmt.Errorf("master key must be %d bytes", MasterKeySize)
	}

	return &Manager{
		store:     artifactStore,
		masterKey: append([]byte(nil), masterKey...),
		random:    rand.Reader,
		now:       time.Now,
	}, nil
}

func (m *Manager) Get(ctx context.Context, sessionName string) (Material, error) {
	record, err := m.store.GetScrubSalt(ctx, sessionName)
	if err != nil {
		return Material{}, err
	}

	return m.decrypt(record)
}

func (m *Manager) GetOrCreate(ctx context.Context, sessionName string) (Material, error) {
	if strings.TrimSpace(sessionName) == "" {
		return Material{}, fmt.Errorf("session name is required")
	}

	unlock := sessionCreationLocks.lock(sessionName)
	defer unlock()

	existing, err := m.Get(ctx, sessionName)
	if err == nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Material{}, err
	}

	salt, err := randomBytes(m.random, SaltSize)
	if err != nil {
		return Material{}, fmt.Errorf("generate session salt for %q: %w", sessionName, err)
	}

	saltID, err := generateSaltID(m.random)
	if err != nil {
		return Material{}, fmt.Errorf("generate salt id for %q: %w", sessionName, err)
	}

	createdAt := m.now().UTC()
	encrypted, err := m.encrypt(sessionName, saltID, salt)
	if err != nil {
		return Material{}, fmt.Errorf("encrypt session salt for %q: %w", sessionName, err)
	}

	record := store.ScrubSalt{
		SessionName:   sessionName,
		SaltID:        saltID,
		SaltEncrypted: encrypted,
		CreatedAt:     createdAt,
	}
	if err := m.store.PutScrubSalt(ctx, record); err != nil {
		return Material{}, fmt.Errorf("persist session salt for %q: %w", sessionName, err)
	}

	return Material{
		SessionName: sessionName,
		SaltID:      saltID,
		Salt:        salt,
		CreatedAt:   createdAt,
	}, nil
}

func Replacement(salt []byte, value string) (string, error) {
	if len(salt) == 0 {
		return "", fmt.Errorf("salt is required")
	}

	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(value))
	token := hex.EncodeToString(mac.Sum(nil)[:8])

	if looksLikeEmail(value) {
		return "user_" + token[:12] + "@scrub.local", nil
	}

	return "hash_" + token, nil
}

func (m *Manager) decrypt(record store.ScrubSalt) (Material, error) {
	if len(record.SaltEncrypted) == 0 {
		return Material{}, fmt.Errorf("encrypted salt payload is empty")
	}

	var envelope encryptedEnvelope
	if err := json.Unmarshal(record.SaltEncrypted, &envelope); err != nil {
		return Material{}, fmt.Errorf("decode encrypted salt envelope for session %q: %w", record.SessionName, err)
	}

	if envelope.Version != envelopeV1 {
		return Material{}, fmt.Errorf("unsupported salt envelope version %q", envelope.Version)
	}

	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return Material{}, fmt.Errorf("decode salt nonce for session %q: %w", record.SessionName, err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return Material{}, fmt.Errorf("decode salt ciphertext for session %q: %w", record.SessionName, err)
	}

	aead, err := m.aead()
	if err != nil {
		return Material{}, err
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, associatedData(record.SessionName, record.SaltID))
	if err != nil {
		return Material{}, fmt.Errorf("decrypt salt for session %q: %w", record.SessionName, err)
	}

	if len(plaintext) != SaltSize {
		return Material{}, fmt.Errorf("decrypted salt for session %q has %d bytes, want %d", record.SessionName, len(plaintext), SaltSize)
	}

	return Material{
		SessionName: record.SessionName,
		SaltID:      record.SaltID,
		Salt:        plaintext,
		CreatedAt:   record.CreatedAt,
	}, nil
}

func (m *Manager) encrypt(sessionName, saltID string, salt []byte) ([]byte, error) {
	aead, err := m.aead()
	if err != nil {
		return nil, err
	}

	nonce, err := randomBytes(m.random, nonceSize)
	if err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nil, nonce, salt, associatedData(sessionName, saltID))
	envelope := encryptedEnvelope{
		Version:    envelopeV1,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}

	return json.Marshal(envelope)
}

func (m *Manager) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(m.masterKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM cipher: %w", err)
	}

	return aead, nil
}

func randomBytes(random io.Reader, size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(random, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func generateSaltID(random io.Reader) (string, error) {
	value, err := randomBytes(random, 8)
	if err != nil {
		return "", err
	}

	return "salt_" + hex.EncodeToString(value), nil
}

func associatedData(sessionName, saltID string) []byte {
	return []byte(sessionName + "|" + saltID + "|" + envelopeV1)
}

func looksLikeEmail(value string) bool {
	return emailPattern.MatchString(strings.TrimSpace(value))
}

func (m *keyedMutexMap) lock(key string) func() {
	m.mu.Lock()
	if m.locks == nil {
		m.locks = make(map[string]*keyedMutexRef)
	}

	ref := m.locks[key]
	if ref == nil {
		ref = &keyedMutexRef{}
		m.locks[key] = ref
	}
	ref.refs++
	m.mu.Unlock()

	ref.mu.Lock()

	return func() {
		ref.mu.Unlock()

		m.mu.Lock()
		defer m.mu.Unlock()

		ref.refs--
		if ref.refs == 0 {
			delete(m.locks, key)
		}
	}
}
