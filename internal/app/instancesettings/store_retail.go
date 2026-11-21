package instancesettings

// StoreRetail is what a workspace pays for domains and mailboxes bought in the
// store: a flat monthly mailbox price, and the domain price GraphKit charges
// marked up by a percent plus a fixed amount.
type StoreRetail struct {
	// Pointers so a document from before this section keeps the compiled defaults.
	MailboxMonthlyMicros    *int64 `json:"mailbox_monthly_micros"`
	DomainMarkupPercent     *int   `json:"domain_markup_percent"`
	DomainMarkupFixedMicros *int64 `json:"domain_markup_fixed_micros"`
	// MailboxBillLeadDays is how many days before a workspace's billing day its mailbox months are collected.
	MailboxBillLeadDays *int `json:"mailbox_bill_lead_days"`
	// MinCardChargeMicros is the least a store charge puts on the card; the rest stays on the balance.
	MinCardChargeMicros *int64 `json:"min_card_charge_micros"`
	// LinkedIn seats a month: every plan but the agency ones, Agency, and Agency Pro.
	LinkedInSeatMonthlyMicros   *int64 `json:"linkedin_seat_monthly_micros"`
	LinkedInSeatAgencyMicros    *int64 `json:"linkedin_seat_agency_micros"`
	LinkedInSeatAgencyProMicros *int64 `json:"linkedin_seat_agency_pro_micros"`
}

const (
	StoreMailboxMonthlyDefault int64 = 3_000_000
	StoreMailboxMonthlyMax     int64 = 100_000_000
	StoreDomainMarkupMax             = 500
	StoreDomainFixedMax        int64 = 100_000_000
	// Mailbox months are collected this many days ahead, so a failed payment has time before GraphKit's period starts.
	StoreBillLeadDefault = 5
	StoreBillLeadMin     = 2
	StoreBillLeadMax     = 10
	// Fewer, larger card charges: the card is charged at least this, topping the balance up.
	StoreMinCardDefault int64 = 10_000_000
	StoreMinCardFloor   int64 = 500_000
	StoreMinCardMax     int64 = 100_000_000
	// LinkedIn seat prices a month, by plan tier.
	LinkedInSeatMonthlyDefault   int64 = 49_000_000
	LinkedInSeatAgencyDefault    int64 = 32_000_000
	LinkedInSeatAgencyProDefault int64 = 19_000_000
	LinkedInSeatMax              int64 = 500_000_000
)

// DefaultStoreRetail sells mailboxes at $3 a month and domains at cost.
func DefaultStoreRetail() StoreRetail {
	monthly, pct, fixed, lead, minCard := StoreMailboxMonthlyDefault, 0, int64(0), StoreBillLeadDefault, StoreMinCardDefault
	seat, agency, agencyPro := LinkedInSeatMonthlyDefault, LinkedInSeatAgencyDefault, LinkedInSeatAgencyProDefault
	return StoreRetail{MailboxMonthlyMicros: &monthly, DomainMarkupPercent: &pct, DomainMarkupFixedMicros: &fixed, MailboxBillLeadDays: &lead,
		MinCardChargeMicros: &minCard, LinkedInSeatMonthlyMicros: &seat, LinkedInSeatAgencyMicros: &agency, LinkedInSeatAgencyProMicros: &agencyPro}
}

// Normalize clamps every price into its accepted range and fills what is missing.
func (s *StoreRetail) Normalize() {
	monthly := StoreMailboxMonthlyDefault
	if s.MailboxMonthlyMicros != nil {
		monthly = max(0, min(*s.MailboxMonthlyMicros, StoreMailboxMonthlyMax))
	}
	pct := 0
	if s.DomainMarkupPercent != nil {
		pct = max(0, min(*s.DomainMarkupPercent, StoreDomainMarkupMax))
	}
	fixed := int64(0)
	if s.DomainMarkupFixedMicros != nil {
		fixed = max(0, min(*s.DomainMarkupFixedMicros, StoreDomainFixedMax))
	}
	lead, minCard := s.MailboxBillLead(), s.MinCardCharge()
	s.MailboxMonthlyMicros, s.DomainMarkupPercent, s.DomainMarkupFixedMicros, s.MailboxBillLeadDays = &monthly, &pct, &fixed, &lead
	s.MinCardChargeMicros = &minCard
	seat, agency, agencyPro := s.LinkedInSeatMonthly(), s.LinkedInSeatAgency(), s.LinkedInSeatAgencyPro()
	s.LinkedInSeatMonthlyMicros, s.LinkedInSeatAgencyMicros, s.LinkedInSeatAgencyProMicros = &seat, &agency, &agencyPro
}

func seatPrice(v *int64, def int64) int64 {
	if v == nil {
		return def
	}
	return max(0, min(*v, LinkedInSeatMax))
}

// LinkedInSeatMonthly is the resolved monthly LinkedIn seat price on every plan but the agency ones.
func (s StoreRetail) LinkedInSeatMonthly() int64 {
	return seatPrice(s.LinkedInSeatMonthlyMicros, LinkedInSeatMonthlyDefault)
}

// LinkedInSeatAgency is the resolved monthly LinkedIn seat price on the Agency plan.
func (s StoreRetail) LinkedInSeatAgency() int64 {
	return seatPrice(s.LinkedInSeatAgencyMicros, LinkedInSeatAgencyDefault)
}

// LinkedInSeatAgencyPro is the resolved monthly LinkedIn seat price on the Agency Pro plan.
func (s StoreRetail) LinkedInSeatAgencyPro() int64 {
	return seatPrice(s.LinkedInSeatAgencyProMicros, LinkedInSeatAgencyProDefault)
}

// MinCardCharge is the resolved least card charge, never under what the card processor accepts.
func (s StoreRetail) MinCardCharge() int64 {
	if s.MinCardChargeMicros == nil {
		return StoreMinCardDefault
	}
	return max(StoreMinCardFloor, min(*s.MinCardChargeMicros, StoreMinCardMax))
}

// MailboxBillLead is the resolved lead in days, clamped to its range.
func (s StoreRetail) MailboxBillLead() int {
	if s.MailboxBillLeadDays == nil {
		return StoreBillLeadDefault
	}
	return max(StoreBillLeadMin, min(*s.MailboxBillLeadDays, StoreBillLeadMax))
}

// MailboxMonthly is the resolved monthly mailbox price.
func (s StoreRetail) MailboxMonthly() int64 {
	if s.MailboxMonthlyMicros == nil {
		return StoreMailboxMonthlyDefault
	}
	return *s.MailboxMonthlyMicros
}

// MarkupPercent is the resolved domain markup percent.
func (s StoreRetail) MarkupPercent() int {
	if s.DomainMarkupPercent == nil {
		return 0
	}
	return *s.DomainMarkupPercent
}

// MarkupFixed is the resolved fixed domain markup.
func (s StoreRetail) MarkupFixed() int64 {
	if s.DomainMarkupFixedMicros == nil {
		return 0
	}
	return *s.DomainMarkupFixedMicros
}
