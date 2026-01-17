package tz

type TzService interface {
	Timezones() []TimezoneOption
}

type tzService struct {
	client *Client
}

func NewService() TzService {
	return &tzService{
		client: NewTZ(),
	}
}

func (s *tzService) Timezones() []TimezoneOption {
	return s.client.Get()
}
