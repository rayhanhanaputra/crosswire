// Package store persists targets, runs and artifacts in one bbolt file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/rayhanhanaputra/crosswire/internal/model"
)

var (
	bTargets   = []byte("targets")
	bRuns      = []byte("runs")
	bArtifacts = []byte("artifacts")
)

// ErrNotFound is returned for a missing key.
var ErrNotFound = errors.New("not found")

// Store is a thin typed wrapper over bbolt.
type Store struct{ db *bolt.DB }

// Open creates or opens the database file.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bTargets, bRuns, bArtifacts} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the file lock.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) put(bucket []byte, key string, v any) error {
	enc, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Put([]byte(key), enc)
	})
}

func (s *Store) get(bucket []byte, key string, v any) error {
	return s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucket).Get([]byte(key))
		if raw == nil {
			return ErrNotFound
		}
		return json.Unmarshal(raw, v)
	})
}

func (s *Store) del(bucket []byte, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucket).Get([]byte(key)) == nil {
			return ErrNotFound
		}
		return tx.Bucket(bucket).Delete([]byte(key))
	})
}

// PutTarget stores a target.
func (s *Store) PutTarget(t model.Target) error { return s.put(bTargets, t.ID, t) }

// GetTarget loads a target.
func (s *Store) GetTarget(id string) (model.Target, error) {
	var t model.Target
	err := s.get(bTargets, id, &t)
	return t, err
}

// DeleteTarget removes a target.
func (s *Store) DeleteTarget(id string) error { return s.del(bTargets, id) }

// ListTargets returns targets sorted by name.
func (s *Store) ListTargets() ([]model.Target, error) {
	var out []model.Target
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bTargets).ForEach(func(_, v []byte) error {
			var t model.Target
			if err := json.Unmarshal(v, &t); err != nil {
				return err
			}
			out = append(out, t)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}

// PutRun stores a run under a time-sortable key so ListRuns is newest-first
// without a secondary index.
func (s *Store) PutRun(r model.Run) error {
	return s.put(bRuns, runKey(r), r)
}

func runKey(r model.Run) string {
	return r.StartedAt.UTC().Format("20060102T150405.000000000") + "_" + r.ID
}

// GetRun loads a run by id (scans keys; runs are few).
func (s *Store) GetRun(id string) (model.Run, error) {
	var found model.Run
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bRuns).Cursor()
		suffix := []byte("_" + id)
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(k) > len(suffix) && string(k[len(k)-len(suffix):]) == string(suffix) {
				return json.Unmarshal(v, &found)
			}
		}
		return ErrNotFound
	})
	return found, err
}

// ListRuns returns up to limit runs, newest first.
func (s *Store) ListRuns(limit int) ([]model.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []model.Run
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bRuns).Cursor()
		for k, v := c.Last(); k != nil && len(out) < limit; k, v = c.Prev() {
			var r model.Run
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

// PutArtifact stores raw bytes (PNG, WAV) under a key.
func (s *Store) PutArtifact(key string, data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bArtifacts).Put([]byte(key), data)
	})
}

// GetArtifact loads raw bytes.
func (s *Store) GetArtifact(key string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bArtifacts).Get([]byte(key))
		if raw == nil {
			return ErrNotFound
		}
		out = append([]byte(nil), raw...)
		return nil
	})
	return out, err
}
