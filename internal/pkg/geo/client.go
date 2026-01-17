package geo

import "github.com/oschwald/geoip2-golang/v2"

type Client struct {
	r *geoip2.Reader
}

func New(location string) (*Client, error) {
	if location == "" {
		return &Client{
			r: nil,
		}, nil
	}
	db, err := geoip2.Open(location)
	if err != nil {
		return nil, err
	}

	return &Client{
		r: db,
	}, nil
}

func (c *Client) Close() {
	_ = c.r.Close()
}
