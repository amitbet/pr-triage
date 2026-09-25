package svc

import "example.com/ws/lib"

type pgStore struct{}

func (p *pgStore) Save(v string) error { return nil }

type Service struct {
	store lib.Store
}

func (s *Service) Handle(e lib.Event) error {
	return s.store.Save(e.ID + lib.Helper())
}

func New() *Service { return &Service{store: &pgStore{}} }
