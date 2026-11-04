package accountcore

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Namespace is the account-core namespace of the PS5 adapter. ConsoleNamespace
// is the link namespace holding one subject per console.
const (
	Namespace        = "ps5"
	ConsoleNamespace = "ps5-console"
)

// ErrNotConfigured is returned by the NotConfigured stub.
var ErrNotConfigured = errors.New("account core not configured")

// Core is what the PS5 services need from the account core. *Client is the
// production implementation; Memory is for tests and local development;
// NotConfigured is the stub a service uses when no core address is set.
type Core interface {
	GetLinkBySubject(ctx context.Context, namespace, subject string) (*Link, error)
	GetActiveLinkByAccount(ctx context.Context, namespace, accountID string) (*Link, error)
	ReserveLink(ctx context.Context, accountID, namespace, subject, idemKey string) (*Link, error)
	ActivateLink(ctx context.Context, linkID, idemKey string) (LinkState, error)
	SetLinkPublicCode(ctx context.Context, linkID, code, lookupKey string) error
	VerifyPassword(ctx context.Context, email, password string) (accountID string, status int, err error)
}

var _ Core = (*Client)(nil)
var _ Core = (*Memory)(nil)
var _ Core = NotConfigured{}

// NotConfigured answers every call with ErrNotConfigured.
type NotConfigured struct{}

func (NotConfigured) GetLinkBySubject(context.Context, string, string) (*Link, error) {
	return nil, ErrNotConfigured
}
func (NotConfigured) GetActiveLinkByAccount(context.Context, string, string) (*Link, error) {
	return nil, ErrNotConfigured
}
func (NotConfigured) ReserveLink(context.Context, string, string, string, string) (*Link, error) {
	return nil, ErrNotConfigured
}
func (NotConfigured) ActivateLink(context.Context, string, string) (LinkState, error) {
	return 0, ErrNotConfigured
}
func (NotConfigured) SetLinkPublicCode(context.Context, string, string, string) error {
	return ErrNotConfigured
}
func (NotConfigured) VerifyPassword(context.Context, string, string) (string, int, error) {
	return "", 0, ErrNotConfigured
}

// Memory is an in-memory Core with the account core's semantics (unknown
// subject is NotFound, activate only from pending, one subject per namespace).
type Memory struct {
	mu        sync.Mutex
	links     map[string]*Link // by id
	accounts  map[string]memAccount
	nextLinkN int
}

type memAccount struct {
	id, password string
	status       int
}

// NewMemory returns an empty Memory.
func NewMemory() *Memory {
	return &Memory{links: map[string]*Link{}, accounts: map[string]memAccount{}}
}

// AddAccount registers a password-verifiable account (status StatusActive for a normal one).
func (m *Memory) AddAccount(email, password, accountID string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts[email] = memAccount{accountID, password, status}
}

func notFound() error { return &Error{Code: CodeNotFound, Message: "not found"} }

func (m *Memory) GetLinkBySubject(_ context.Context, namespace, subject string) (*Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.links {
		if l.nsKey() == namespace && l.SubjectID == subject {
			c := *l
			return &c, nil
		}
	}
	return nil, notFound()
}

func (m *Memory) GetActiveLinkByAccount(_ context.Context, namespace, accountID string) (*Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.links {
		if l.nsKey() == namespace && l.AccountID == accountID && l.State == StateActive {
			c := *l
			return &c, nil
		}
	}
	return nil, notFound()
}

func (m *Memory) ReserveLink(_ context.Context, accountID, namespace, subject, _ string) (*Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.links {
		if l.nsKey() == namespace && l.SubjectID == subject && l.State != StateRevoked {
			return nil, &Error{Code: CodeAlreadyExists, Message: "subject already linked"}
		}
	}
	m.nextLinkN++
	l := &Link{LinkID: fmt.Sprintf("L%d", m.nextLinkN), AccountID: accountID, SubjectID: subject, State: StatePending, ns: namespace}
	m.links[l.LinkID] = l
	c := *l
	return &c, nil
}

func (m *Memory) ActivateLink(_ context.Context, linkID, _ string) (LinkState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[linkID]
	if !ok {
		return 0, notFound()
	}
	if l.State != StatePending && l.State != StateActive {
		return 0, &Error{Code: CodeFailedPrecond, Message: "not pending"}
	}
	l.State = StateActive
	return l.State, nil
}

func (m *Memory) SetLinkPublicCode(_ context.Context, linkID, code, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[linkID]
	if !ok {
		return notFound()
	}
	if l.State != StateActive {
		return &Error{Code: CodeFailedPrecond, Message: "not active"}
	}
	l.PublicCode = code
	return nil
}

func (m *Memory) VerifyPassword(_ context.Context, email, password string) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[email]
	if !ok || a.password != password {
		return "", 0, &Error{Code: CodeUnauthenticated, Message: "invalid credentials"}
	}
	if a.status != StatusActive {
		return "", a.status, nil
	}
	return a.id, a.status, nil
}

// nsKey is the namespace a Memory link was reserved in.
func (l *Link) nsKey() string { return l.ns }
