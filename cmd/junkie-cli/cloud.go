package main

import "net/url"

// cloudStore is the signed-in desk: the same HTTP client and the same
// sockets the dashboard already had, hidden behind store so the screen
// can also run against local files.
type cloudStore struct {
	client  *client
	events  chan signal
	sockets *sockets
	rooms   []string
}

func newCloudStore(c *client) *cloudStore {
	return &cloudStore{
		client: c,
		events: make(chan signal, 32),
	}
}

func (s *cloudStore) Load() (deskResponse, error) {
	desk, err := s.client.desk()
	if err != nil {
		return desk, err
	}
	s.subscribe(deskRoomCodes(desk))
	return desk, nil
}

func (s *cloudStore) Do(path string, form url.Values) error {
	return s.client.post(path, form)
}

func (s *cloudStore) Profile() (profileResponse, error) {
	return s.client.profile()
}

func (s *cloudStore) Events() <-chan signal { return s.events }

func (s *cloudStore) Close() {
	if s.sockets != nil {
		s.sockets.close()
		s.sockets = nil
	}
}

func (s *cloudStore) subscribe(codes []string) {
	if s.sockets != nil && sameCodes(codes, s.rooms) {
		return
	}
	if s.sockets != nil {
		s.sockets.close()
	}
	s.rooms = codes
	s.sockets = openSocketsOnto(s.client, codes, s.events)
}
