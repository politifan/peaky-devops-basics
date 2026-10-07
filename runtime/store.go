package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Ticket struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
type savedTicket struct {
	Ticket
	Key string `json:"key,omitempty"`
}
type Store struct {
	mu       sync.Mutex
	path     string
	db       *sql.DB
	records  map[string]savedTicket
	keys     map[string]string
	fileLock *os.File
}

var conflict = errors.New("idempotency_conflict")
var notfound = errors.New("not_found")

func openStore(kind, dir string) (*Store, error) {
	s := &Store{records: map[string]savedTicket{}, keys: map[string]string{}}
	if kind == "postgresql" {
		u := os.Getenv("TICKETLAB_DATABASE_URL")
		if u == "" {
			return nil, errors.New("TICKETLAB_DATABASE_URL не задан")
		}
		db, e := sql.Open("pgx", u)
		if e != nil {
			return nil, errors.New("некорректная настройка PostgreSQL")
		}
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(2)
		s.db = db
		return s, nil
	}
	if kind != "file" {
		return nil, errors.New("storage должен быть file или postgresql")
	}
	if !filepath.IsAbs(dir) {
		return nil, errors.New("data-dir должен быть абсолютным")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s.path = filepath.Join(dir, "tickets.json")
	locked, e := storeLock(filepath.Join(dir, ".writer.lock"))
	if e != nil {
		return nil, errors.New("data-dir уже занят другим пишущим процессом")
	}
	s.fileLock = locked
	b, e := os.ReadFile(s.path)
	if e == nil {
		var list []savedTicket
		if e = json.Unmarshal(b, &list); e != nil {
			s.close()
			return nil, errors.New("невалидный файл данных; оригинал не изменён")
		}
		for _, t := range list {
			s.records[t.ID] = t
			if t.Key != "" {
				s.keys[t.Key] = t.ID
			}
		}
	} else if !os.IsNotExist(e) {
		s.close()
		return nil, e
	}
	return s, nil
}
func (s *Store) ready() error {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var rev int
		e := s.db.QueryRowContext(ctx, "SELECT revision FROM ticketlab_schema WHERE singleton=true").Scan(&rev)
		if e != nil || rev != 1 {
			return errors.New("dependency_or_schema_unavailable")
		}
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, e := os.ReadFile(s.path); e != nil {
		if !os.IsNotExist(e) || len(s.records) > 0 {
			return errors.New("data_file_unavailable")
		}
	} else if !json.Valid(b) {
		return errors.New("data_file_invalid")
	}
	f, e := os.CreateTemp(filepath.Dir(s.path), ".ready-")
	if e != nil {
		return e
	}
	n := f.Name()
	e = f.Close()
	os.Remove(n)
	return e
}
func (s *Store) migrate(apply bool) error {
	if s.db == nil {
		return errors.New("миграция требует настоящий PostgreSQL")
	}
	if !apply {
		return s.ready()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return errors.New("PostgreSQL недоступен")
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ticketlab_schema(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), revision integer NOT NULL);
 INSERT INTO ticketlab_schema(singleton,revision) VALUES(true,1) ON CONFLICT(singleton) DO NOTHING;
 CREATE TABLE IF NOT EXISTS tickets(id text PRIMARY KEY,title text NOT NULL,status text NOT NULL CHECK(status IN ('open','closed')),idempotency_key text UNIQUE);`)
	if e != nil {
		return errors.New("миграция не выполнена; транзакция отменена")
	}
	var rev int
	if e = tx.QueryRowContext(ctx, "SELECT revision FROM ticketlab_schema").Scan(&rev); e != nil || rev != 1 {
		return errors.New("неподдерживаемая schema revision")
	}
	return tx.Commit()
}
func (s *Store) save() error {
	list := make([]savedTicket, 0, len(s.records))
	for _, t := range s.records {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return atomicJSON(s.path, list, false)
}
func (s *Store) create(title, key string) (Ticket, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		t := Ticket{marker(), title, "open"}
		var k any
		if key != "" {
			k = key
		}
		result, e := s.db.ExecContext(ctx, "INSERT INTO tickets(id,title,status,idempotency_key) VALUES($1,$2,$3,$4) ON CONFLICT(idempotency_key) DO NOTHING", t.ID, t.Title, t.Status, k)
		if e != nil {
			return Ticket{}, false, errors.New("storage_unavailable")
		}
		n, _ := result.RowsAffected()
		if n == 1 {
			return t, false, nil
		}
		e = s.db.QueryRowContext(ctx, "SELECT id,title,status FROM tickets WHERE idempotency_key=$1", key).Scan(&t.ID, &t.Title, &t.Status)
		if e != nil {
			return Ticket{}, false, errors.New("storage_unavailable")
		}
		if t.Title != title {
			return Ticket{}, false, conflict
		}
		return t, true, nil
	}
	if id, ok := s.keys[key]; key != "" && ok {
		t := s.records[id]
		if t.Title != title {
			return Ticket{}, false, conflict
		}
		return t.Ticket, true, nil
	}
	t := Ticket{marker(), title, "open"}
	s.records[t.ID] = savedTicket{t, key}
	if key != "" {
		s.keys[key] = t.ID
	}
	if e := s.save(); e != nil {
		delete(s.records, t.ID)
		delete(s.keys, key)
		return Ticket{}, false, e
	}
	return t, false, nil
}
func (s *Store) get(id string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		var t Ticket
		e := s.db.QueryRowContext(ctx, "SELECT id,title,status FROM tickets WHERE id=$1", id).Scan(&t.ID, &t.Title, &t.Status)
		if e == sql.ErrNoRows {
			return t, notfound
		}
		return t, e
	}
	t, ok := s.records[id]
	if !ok {
		return Ticket{}, notfound
	}
	return t.Ticket, nil
}
func (s *Store) update(id, status string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		var t Ticket
		e := s.db.QueryRowContext(ctx, "UPDATE tickets SET status=$2 WHERE id=$1 RETURNING id,title,status", id, status).Scan(&t.ID, &t.Title, &t.Status)
		if e == sql.ErrNoRows {
			return t, notfound
		}
		return t, e
	}
	t, ok := s.records[id]
	if !ok {
		return Ticket{}, notfound
	}
	before := t
	t.Status = status
	s.records[id] = t
	if e := s.save(); e != nil {
		s.records[id] = before
		return Ticket{}, e
	}
	return t.Ticket, nil
}
func (s *Store) list(limit, offset int) ([]Ticket, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []Ticket{}
	if s.db != nil {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		var total int
		if e := s.db.QueryRowContext(ctx, "SELECT count(*) FROM tickets").Scan(&total); e != nil {
			return nil, 0, e
		}
		rows, e := s.db.QueryContext(ctx, "SELECT id,title,status FROM tickets ORDER BY id LIMIT $1 OFFSET $2", limit, offset)
		if e != nil {
			return nil, 0, e
		}
		defer rows.Close()
		for rows.Next() {
			var t Ticket
			if e = rows.Scan(&t.ID, &t.Title, &t.Status); e != nil {
				return nil, 0, e
			}
			list = append(list, t)
		}
		return list, total, rows.Err()
	}
	ids := []string{}
	for id := range s.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i := offset; i < len(ids) && i < offset+limit; i++ {
		list = append(list, s.records[ids[i]].Ticket)
	}
	return list, len(ids), nil
}
func (s *Store) close() {
	if s.fileLock != nil {
		s.fileLock.Close()
	}
	if s.db != nil {
		s.db.Close()
	}
}
func storageError(e error) string {
	if e == notfound {
		return "not_found"
	}
	if e == conflict {
		return "idempotency_conflict"
	}
	return fmt.Sprint("storage_unavailable")
}
