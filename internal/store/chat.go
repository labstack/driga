package store

import (
	"context"
	"time"
)

// Chat text is already capture-filtered by the broker before these writes.
type Conversation struct {
	ID        string    `json:"id"`
	MachineID string    `json:"machine_id"`
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type ChatMessage struct {
	Tool           string    `json:"tool,omitempty"`
	DecidedBy      string    `json:"decided_by,omitempty"`
	ID             string    `json:"id"`
	MachineID      string    `json:"machine_id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Text           string    `json:"text"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Store) SaveConversation(ctx context.Context, c Conversation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations(id,machine_id,agent,session_id,created_at,updated_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id,updated_at=excluded.updated_at`, c.ID, s.MachineID(), c.Agent, c.SessionID, unix(c.CreatedAt), unix(c.UpdatedAt))
	return err
}
func (s *Store) ChatConversations() ([]Conversation, error) {
	rows, err := s.db.QueryContext(s.ctx, `SELECT id,machine_id,agent,session_id,created_at,updated_at FROM conversations WHERE machine_id=? ORDER BY updated_at DESC LIMIT 100`, s.MachineID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		var created, updated int64
		if err := rows.Scan(&c.ID, &c.MachineID, &c.Agent, &c.SessionID, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt = fromUnix(created)
		c.UpdatedAt = fromUnix(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) ChatConversation(id string) (Conversation, error) {
	var c Conversation
	var a, b int64
	err := s.db.QueryRowContext(s.ctx, `SELECT id,machine_id,agent,session_id,created_at,updated_at FROM conversations WHERE id=? AND machine_id=?`, id, s.MachineID()).Scan(&c.ID, &c.MachineID, &c.Agent, &c.SessionID, &a, &b)
	c.CreatedAt = fromUnix(a)
	c.UpdatedAt = fromUnix(b)
	return c, err
}
func (s *Store) SaveChatMessage(ctx context.Context, m ChatMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages(id,machine_id,conversation_id,role,text,status,created_at,tool,decided_by) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET text=excluded.text,status=excluded.status,tool=excluded.tool,decided_by=excluded.decided_by`, m.ID, s.MachineID(), m.ConversationID, m.Role, m.Text, m.Status, unix(m.CreatedAt), m.Tool, m.DecidedBy)
	return err
}
func (s *Store) ChatMessages(id string) ([]ChatMessage, error) {
	rows, err := s.db.QueryContext(s.ctx, `SELECT id,machine_id,conversation_id,role,text,status,created_at,tool,decided_by FROM (SELECT * FROM messages WHERE conversation_id=? AND machine_id=? ORDER BY created_at DESC LIMIT 256) ORDER BY created_at`, id, s.MachineID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var at int64
		if err := rows.Scan(&m.ID, &m.MachineID, &m.ConversationID, &m.Role, &m.Text, &m.Status, &at, &m.Tool, &m.DecidedBy); err != nil {
			return nil, err
		}
		m.CreatedAt = fromUnix(at)
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) InterruptChat(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET status=CASE WHEN role='action' THEN 'denied' ELSE 'interrupted' END WHERE machine_id=? AND status IN ('queued','streaming')`, s.MachineID())
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE messages SET status=CASE status WHEN 'pending' THEN 'denied' ELSE 'failed' END, text=CASE WHEN text='' THEN '' ELSE replace(text, ' · ' || status, CASE status WHEN 'pending' THEN ' · denied' ELSE ' · failed' END) || ' · interrupted' END WHERE machine_id=? AND role='action' AND status IN ('pending','allowed')`, s.MachineID())
	return err
}

// RedactChat also handles a policy downgrade before a broker serves history.
func (s *Store) RedactChat(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET text='' WHERE machine_id=? AND role!='tool'`, s.MachineID())
	return err
}
