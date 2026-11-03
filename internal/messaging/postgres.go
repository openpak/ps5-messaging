package messaging

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openpak/ps5-common/pgdb"
)

// Schema is the idempotent DDL of the durable groups and message history.
const Schema = `
CREATE TABLE IF NOT EXISTS ps5_msg_groups (
	seq        bigserial PRIMARY KEY,
	name       text NOT NULL DEFAULT '',
	kind       int NOT NULL,
	created_at timestamptz NOT NULL,
	modified   timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS ps5_msg_members (
	group_seq  bigint NOT NULL REFERENCES ps5_msg_groups (seq),
	account_id text NOT NULL,
	online_id  text NOT NULL DEFAULT '',
	position   int NOT NULL,
	PRIMARY KEY (group_seq, account_id)
);
CREATE INDEX IF NOT EXISTS ps5_msg_members_account ON ps5_msg_members (account_id);
CREATE TABLE IF NOT EXISTS ps5_msg_messages (
	uid        bigserial PRIMARY KEY,
	group_seq  bigint NOT NULL REFERENCES ps5_msg_groups (seq),
	kind       int NOT NULL,
	body       text NOT NULL,
	created_at timestamptz NOT NULL,
	sender_id  text NOT NULL,
	sender_online text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS ps5_msg_messages_group ON ps5_msg_messages (group_seq, uid);
`

const opTimeout = 5 * time.Second

// Postgres is the durable Store: groups, members and every message live in Postgres. Group ids are
// "g<seq>" and the single thread of a group is "t<seq>", the same shapes Memory hands out.
type Postgres struct{ pool *pgxpool.Pool }

var _ Store = (*Postgres)(nil)

// NewPostgres migrates the schema and returns the store. The caller owns the pool.
func NewPostgres(ctx context.Context, pool *pgxpool.Pool) (*Postgres, error) {
	if err := pgdb.Migrate(ctx, pool, Schema); err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func ctx5() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

// seqOf parses "g12" / "t12" into 12.
func seqOf(id, prefix string) (int64, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.ParseInt(id[len(prefix):], 10, 64)
	return n, err == nil && n > 0
}

func (p *Postgres) CreateGroup(creator Member, invitees []Member, name string) (Group, error) {
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
	typ := 1
	if len(members) == 2 {
		typ = 0
	}
	ctx, cancel := ctx5()
	defer cancel()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback(ctx)
	var seq int64
	var t time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO ps5_msg_groups (name, kind, created_at, modified) VALUES ($1,$2,now(),now()) RETURNING seq, created_at`, name, typ).Scan(&seq, &t); err != nil {
		return Group{}, err
	}
	for i, m := range members {
		if _, err := tx.Exec(ctx, `INSERT INTO ps5_msg_members (group_seq, account_id, online_id, position) VALUES ($1,$2,$3,$4)`, seq, m.AccountID, m.OnlineID, i); err != nil {
			return Group{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, err
	}
	id := "g" + strconv.FormatInt(seq, 10)
	return Group{GroupID: id, GroupName: name, GroupType: typ, Members: members,
		MainThread: Thread{ThreadID: "t" + id[1:]}, CreatedTimestamp: stamp(t), Modified: stamp(t)}, nil
}

type q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// load builds the groups with the given sequence numbers (members and latest message included).
func load(ctx context.Context, db q, where string, args ...any) ([]Group, error) {
	rows, err := db.Query(ctx, `SELECT g.seq, g.name, g.kind, g.created_at, g.modified FROM ps5_msg_groups g `+where, args...)
	if err != nil {
		return nil, err
	}
	var out []Group
	var seqs []int64
	for rows.Next() {
		var g Group
		var seq int64
		var c, m time.Time
		if err := rows.Scan(&seq, &g.GroupName, &g.GroupType, &c, &m); err != nil {
			rows.Close()
			return nil, err
		}
		g.GroupID = "g" + strconv.FormatInt(seq, 10)
		g.MainThread.ThreadID = "t" + strconv.FormatInt(seq, 10)
		g.CreatedTimestamp, g.Modified = stamp(c), stamp(m)
		out = append(out, g)
		seqs = append(seqs, seq)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, seq := range seqs {
		mr, err := db.Query(ctx, `SELECT account_id, online_id FROM ps5_msg_members WHERE group_seq=$1 ORDER BY position`, seq)
		if err != nil {
			return nil, err
		}
		for mr.Next() {
			var m Member
			if err := mr.Scan(&m.AccountID, &m.OnlineID); err != nil {
				mr.Close()
				return nil, err
			}
			out[i].Members = append(out[i].Members, m)
		}
		mr.Close()
		if err := mr.Err(); err != nil {
			return nil, err
		}
		var m Message
		var c time.Time
		var uid int64
		err = db.QueryRow(ctx, `SELECT uid, kind, body, created_at, sender_id, sender_online FROM ps5_msg_messages WHERE group_seq=$1 ORDER BY uid DESC LIMIT 1`, seq).
			Scan(&uid, &m.MessageType, &m.Body, &c, &m.Sender.AccountID, &m.Sender.OnlineID)
		if err == nil {
			m.MessageUID, m.CreatedTimestamp = strconv.FormatInt(uid, 10), stamp(c)
			out[i].MainThread.LatestMessage = &m
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return out, nil
}

func (p *Postgres) GroupsOf(accountID string) []Group {
	ctx, cancel := ctx5()
	defer cancel()
	gs, err := load(ctx, p.pool, `WHERE g.seq IN (SELECT group_seq FROM ps5_msg_members WHERE account_id=$1) ORDER BY g.modified DESC, g.seq DESC`, accountID)
	if err != nil || gs == nil {
		return []Group{}
	}
	return gs
}

func (p *Postgres) isMember(ctx context.Context, seq int64, acct string) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ps5_msg_members WHERE group_seq=$1 AND account_id=$2)`, seq, acct).Scan(&ok)
	return ok, err
}

func (p *Postgres) exists(ctx context.Context, seq int64) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ps5_msg_groups WHERE seq=$1)`, seq).Scan(&ok)
	return ok, err
}

// check validates the group, thread and membership the way Memory does.
func (p *Postgres) check(ctx context.Context, groupID, threadID, acct string) (int64, error) {
	seq, ok := seqOf(groupID, "g")
	if !ok {
		return 0, ErrNotFound
	}
	if tseq, ok := seqOf(threadID, "t"); !ok || tseq != seq {
		return 0, ErrNotFound
	}
	if found, err := p.exists(ctx, seq); err != nil {
		return 0, err
	} else if !found {
		return 0, ErrNotFound
	}
	if mem, err := p.isMember(ctx, seq, acct); err != nil {
		return 0, err
	} else if !mem {
		return 0, ErrNotMember
	}
	return seq, nil
}

func (p *Postgres) Group(groupID, accountID string) (Group, error) {
	ctx, cancel := ctx5()
	defer cancel()
	seq, ok := seqOf(groupID, "g")
	if !ok {
		return Group{}, ErrNotFound
	}
	if found, err := p.exists(ctx, seq); err != nil {
		return Group{}, err
	} else if !found {
		return Group{}, ErrNotFound
	}
	if mem, err := p.isMember(ctx, seq, accountID); err != nil {
		return Group{}, err
	} else if !mem {
		return Group{}, ErrNotMember
	}
	gs, err := load(ctx, p.pool, `WHERE g.seq=$1`, seq)
	if err != nil || len(gs) != 1 {
		if err == nil {
			err = ErrNotFound
		}
		return Group{}, err
	}
	return gs[0], nil
}

func (p *Postgres) Post(groupID, threadID string, sender Member, msgType int, body string) (Message, error) {
	ctx, cancel := ctx5()
	defer cancel()
	seq, err := p.check(ctx, groupID, threadID, sender.AccountID)
	if err != nil {
		return Message{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)
	var uid int64
	var t time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO ps5_msg_messages (group_seq, kind, body, created_at, sender_id, sender_online) VALUES ($1,$2,$3,now(),$4,$5) RETURNING uid, created_at`,
		seq, msgType, body, sender.AccountID, sender.OnlineID).Scan(&uid, &t); err != nil {
		return Message{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ps5_msg_groups SET modified=$2 WHERE seq=$1`, seq, t); err != nil {
		return Message{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ps5_msg_members SET online_id=$3 WHERE group_seq=$1 AND account_id=$2 AND online_id=''`, seq, sender.AccountID, sender.OnlineID); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	return Message{MessageUID: strconv.FormatInt(uid, 10), MessageType: msgType, Body: body, CreatedTimestamp: stamp(t), Sender: sender}, nil
}

func (p *Postgres) Messages(groupID, threadID, accountID string, limit int) ([]Message, int, error) {
	ctx, cancel := ctx5()
	defer cancel()
	seq, err := p.check(ctx, groupID, threadID, accountID)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM ps5_msg_messages WHERE group_seq=$1`, seq).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := p.pool.Query(ctx, `SELECT uid, kind, body, created_at, sender_id, sender_online FROM ps5_msg_messages WHERE group_seq=$1 ORDER BY uid DESC LIMIT $2`, seq, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var uid int64
		var c time.Time
		if err := rows.Scan(&uid, &m.MessageType, &m.Body, &c, &m.Sender.AccountID, &m.Sender.OnlineID); err != nil {
			return nil, 0, err
		}
		m.MessageUID, m.CreatedTimestamp = strconv.FormatInt(uid, 10), stamp(c)
		out = append(out, m)
	}
	return out, total, rows.Err()
}
