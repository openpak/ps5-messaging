// Package messaging implements the PSN groups and messages API (gamingLoungeGroups).
package messaging

import (
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Member is a group member.
type Member struct {
	AccountID string `json:"accountId"`
	OnlineID  string `json:"onlineId,omitempty"`
}

// Message is one message in a thread.
type Message struct {
	MessageUID       string `json:"messageUid"`
	MessageType      int    `json:"messageType"`
	AltMessageType   int    `json:"alternativeMessageType"`
	Body             string `json:"body"`
	CreatedTimestamp string `json:"createdTimestamp"`
	Sender           Member `json:"sender"`
}

// Group is a group (or direct message) with its single main thread.
type Group struct {
	GroupID          string    `json:"groupId"`
	GroupName        string    `json:"groupName,omitempty"`
	GroupType        int       `json:"groupType"` // 0 direct message, 1 group
	Members          []Member  `json:"members"`
	MainThread       Thread    `json:"mainThread"`
	CreatedTimestamp string    `json:"createdTimestamp"`
	Modified         string    `json:"modifiedTimestamp"`
	created          time.Time `json:"-"`
}

// Thread is the thread descriptor inside a group.
type Thread struct {
	ThreadID      string   `json:"threadId"`
	LatestMessage *Message `json:"latestMessage,omitempty"`
}

var (
	ErrNotFound  = errors.New("not found")
	ErrNotMember = errors.New("not a member")
)

// Store is the storage the service needs. The intended production backend is the
// OpenPak universal social/chat service (platforms/core/infra/chat); Memory serves
// tests and development.
type Store interface {
	CreateGroup(creator Member, invitees []Member, name string) (Group, error)
	GroupsOf(accountID string) []Group
	Group(groupID, accountID string) (Group, error)
	Post(groupID, threadID string, sender Member, msgType int, body string) (Message, error)
	Messages(groupID, threadID, accountID string, limit int) ([]Message, int, error) // newest first, total count
}

// Memory is an in-memory Store.
type Memory struct {
	mu     sync.Mutex
	now    func() time.Time
	seq    uint64
	groups map[string]*Group
	msgs   map[string][]Message // by group id (one thread per group)
}

func NewMemory() *Memory {
	return &Memory{now: time.Now, groups: map[string]*Group{}, msgs: map[string][]Message{}}
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func (m *Memory) next() string { m.seq++; return strconv.FormatUint(m.seq, 10) }

func (m *Memory) CreateGroup(creator Member, invitees []Member, name string) (Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	members := []Member{creator}
	seen := map[string]bool{creator.AccountID: true}
	for _, i := range invitees {
		if i.AccountID != "" && !seen[i.AccountID] {
			seen[i.AccountID] = true
			members = append(members, Member{AccountID: i.AccountID})
		}
	}
	if len(members) < 2 {
		return Group{}, errors.New("a group needs at least one invitee")
	}
	t := m.now()
	typ := 1
	if len(members) == 2 {
		typ = 0
	}
	id := "g" + m.next()
	g := &Group{GroupID: id, GroupName: name, GroupType: typ, Members: members,
		MainThread: Thread{ThreadID: "t" + id[1:]}, CreatedTimestamp: stamp(t), Modified: stamp(t), created: t}
	m.groups[id] = g
	return *g, nil
}

func isMember(g *Group, acct string) bool {
	for _, mb := range g.Members {
		if mb.AccountID == acct {
			return true
		}
	}
	return false
}

func (m *Memory) GroupsOf(accountID string) []Group {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Group{}
	for _, g := range m.groups {
		if isMember(g, accountID) {
			out = append(out, m.view(g))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Modified > out[j].Modified || out[i].Modified == out[j].Modified && out[i].GroupID > out[j].GroupID
	})
	return out
}

func (m *Memory) view(g *Group) Group {
	v := *g
	if ms := m.msgs[g.GroupID]; len(ms) > 0 {
		last := ms[len(ms)-1]
		v.MainThread.LatestMessage = &last
	}
	return v
}

func (m *Memory) Group(groupID, accountID string) (Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[groupID]
	if !ok {
		return Group{}, ErrNotFound
	}
	if !isMember(g, accountID) {
		return Group{}, ErrNotMember
	}
	return m.view(g), nil
}

func (m *Memory) check(groupID, threadID, acct string) (*Group, error) {
	g, ok := m.groups[groupID]
	if !ok || g.MainThread.ThreadID != threadID {
		return nil, ErrNotFound
	}
	if !isMember(g, acct) {
		return nil, ErrNotMember
	}
	return g, nil
}

func (m *Memory) Post(groupID, threadID string, sender Member, msgType int, body string) (Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, err := m.check(groupID, threadID, sender.AccountID)
	if err != nil {
		return Message{}, err
	}
	t := m.now()
	msg := Message{MessageUID: m.next(), MessageType: msgType, Body: body, CreatedTimestamp: stamp(t), Sender: sender}
	m.msgs[groupID] = append(m.msgs[groupID], msg)
	g.Modified = stamp(t)
	for i := range g.Members {
		if g.Members[i].AccountID == sender.AccountID && g.Members[i].OnlineID == "" {
			g.Members[i].OnlineID = sender.OnlineID
		}
	}
	return msg, nil
}

func (m *Memory) Messages(groupID, threadID, accountID string, limit int) ([]Message, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.check(groupID, threadID, accountID); err != nil {
		return nil, 0, err
	}
	all := m.msgs[groupID]
	out := []Message{}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, all[i])
	}
	return out, len(all), nil
}
