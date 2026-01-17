package geo

import (
	"net/netip"
)

type Info struct {
	City        string
	Country     string
	CountryCode string
	Region      string
	PostalCode  string
}

func (c *Client) Lookup(ip netip.Addr) (*Info, error) {
	info := &Info{}

	if ip.IsPrivate() || ip.IsLoopback() {
		info.City = "Local"
		info.Country = "Local Network"
		return info, nil
	}

	if c.r != nil {
		cityRecord, err := c.r.City(ip)
		if err == nil && cityRecord != nil {
			info.City = cityRecord.City.Names.English
			if len(cityRecord.Subdivisions) > 0 {
				info.Region = cityRecord.Subdivisions[0].Names.English
			}
			info.Country = cityRecord.Country.Names.English
			info.CountryCode = cityRecord.Country.ISOCode
			info.PostalCode = cityRecord.Postal.Code
		}
	}

	if info.City == "" {
		info.City = "Unknown"
	}

	if info.Country == "" {
		info.Country = "Unknown"
	}

	return info, nil
}
