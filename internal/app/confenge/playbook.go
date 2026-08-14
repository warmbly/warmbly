package confenge

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// OutreachDoctrineVersion is stamped on every strategy and draft metadata.
const OutreachDoctrineVersion = "confenge-outreach-v2"

//go:embed outreach_playbook/*.yaml
var playbookFS embed.FS

// Playbook is the machine-readable commercial doctrine.
type Playbook struct {
	Doctrine   DoctrineFile   `yaml:"-"`
	Personas   PersonasFile   `yaml:"-"`
	Services   ServicesFile   `yaml:"-"`
	Triggers   TriggersFile   `yaml:"-"`
	Offers     OffersFile     `yaml:"-"`
	Sequence   SequenceFile   `yaml:"-"`
	Objections ObjectionsFile `yaml:"-"`
	CopyRules  CopyRulesFile  `yaml:"-"`
}

type DoctrineFile struct {
	OutreachDoctrineVersion string `yaml:"outreach_doctrine_version"`
	ChannelPolicy           struct {
		RuntimeAllowed []string `yaml:"runtime_allowed"`
		WhatsApp       string   `yaml:"whatsapp"`
		AutoCall       string   `yaml:"auto_call"`
		GreenAutorun   string   `yaml:"green_autorun"`
	} `yaml:"channel_policy"`
	NorthStarMetric    string   `yaml:"north_star_metric"`
	SecondaryMetrics   []string `yaml:"secondary_metrics"`
	FirstEmailDefaults struct {
		MinWords          int    `yaml:"min_words"`
		MaxWords          int    `yaml:"max_words"`
		MaxCTAs           int    `yaml:"max_ctas"`
		MaxLinks          int    `yaml:"max_links"`
		Attachments       bool   `yaml:"attachments"`
		MeetingCTADefault bool   `yaml:"meeting_cta_default"`
		Language          string `yaml:"language"`
	} `yaml:"first_email_defaults"`
}

type PersonasFile struct {
	OutreachDoctrineVersion string        `yaml:"outreach_doctrine_version"`
	Roles                   []PersonaRole `yaml:"roles"`
}

type PersonaRole struct {
	Code          string   `yaml:"code"`
	MatchKeywords []string `yaml:"match_keywords"`
	Priorities    []string `yaml:"priorities"`
	Tone          string   `yaml:"tone"`
	PreferAngles  []string `yaml:"prefer_angles"`
	Rules         []string `yaml:"rules"`
}

type ServicesFile struct {
	OutreachDoctrineVersion string            `yaml:"outreach_doctrine_version"`
	Services                []ServicePlaybook `yaml:"services"`
}

type ServicePlaybook struct {
	Code               string   `yaml:"code"`
	Name               string   `yaml:"name"`
	Aliases            []string `yaml:"aliases"`
	CommercialInsight  string   `yaml:"commercial_insight"`
	CommonMiss         string   `yaml:"common_miss"`
	ProblemHypotheses  []string `yaml:"problem_hypotheses"`
	Implications       []string `yaml:"implications"`
	DisallowedClaims   []string `yaml:"disallowed_claims"`
	DefaultMicroOffer  string   `yaml:"default_micro_offer"`
	CTAFamilies        []string `yaml:"cta_families"`
	CreditVocabAllowed bool     `yaml:"credit_vocab_allowed"`
	OutboundHookClass  string   `yaml:"outbound_hook_class"`
}

type TriggersFile struct {
	OutreachDoctrineVersion string        `yaml:"outreach_doctrine_version"`
	Triggers                []TriggerRule `yaml:"triggers"`
}

type TriggerRule struct {
	Code            string   `yaml:"code"`
	Aliases         []string `yaml:"aliases"`
	WhyNowTemplate  string   `yaml:"why_now_template"`
	FactVsClaim     string   `yaml:"fact_vs_claim"`
	PreferredOffers []string `yaml:"preferred_offers"`
	RiskFlags       []string `yaml:"risk_flags"`
	ClaimsToAvoid   []string `yaml:"claims_to_avoid"`
}

type OffersFile struct {
	OutreachDoctrineVersion string           `yaml:"outreach_doctrine_version"`
	Offers                  []MicroOfferDef  `yaml:"offers"`
	RestrictedOffers        []map[string]any `yaml:"restricted_offers"`
}

type MicroOfferDef struct {
	Code                   string   `yaml:"code"`
	Description            string   `yaml:"description"`
	BuyerValue             string   `yaml:"buyer_value"`
	FulfillmentPath        string   `yaml:"fulfillment_path"`
	FulfillmentCost        string   `yaml:"fulfillment_cost"`
	RequiredEvidence       []string `yaml:"required_evidence"`
	ApplicableServiceCodes []string `yaml:"applicable_service_codes"`
	ProhibitedClaims       []string `yaml:"prohibited_claims"`
	CTAPatterns            []string `yaml:"cta_patterns"`
}

type SequenceFile struct {
	OutreachDoctrineVersion string `yaml:"outreach_doctrine_version"`
	Sequence                struct {
		MaxTouches int `yaml:"max_touches"`
		Touches    []struct {
			Position     int      `yaml:"position"`
			Name         string   `yaml:"name"`
			Objective    string   `yaml:"objective"`
			AddsValue    string   `yaml:"adds_value"`
			Forbidden    []string `yaml:"forbidden"`
			DefaultDelay int      `yaml:"default_delay_days"`
		} `yaml:"touches"`
	} `yaml:"sequence"`
	EmptyFollowupPhrases []string `yaml:"empty_followup_phrases"`
}

type ObjectionsFile struct {
	OutreachDoctrineVersion string `yaml:"outreach_doctrine_version"`
	Objections              []struct {
		ID       string   `yaml:"id"`
		Match    []string `yaml:"match"`
		Strategy string   `yaml:"strategy"`
		Guidance string   `yaml:"guidance"`
		Never    []string `yaml:"never"`
	} `yaml:"objections"`
}

type CopyRulesFile struct {
	OutreachDoctrineVersion string   `yaml:"outreach_doctrine_version"`
	BannedPhrases           []string `yaml:"banned_phrases"`
	MeetingCTAPhrases       []string `yaml:"meeting_cta_phrases"`
	CreepyPhrasing          []string `yaml:"creepy_phrasing"`
	UnsupportedMonetary     []string `yaml:"unsupported_monetary"`
	AnnualidadeBadClaims    []string `yaml:"annualidade_bad_claims"`
	RejectionReasons        []string `yaml:"rejection_reasons"`
	EditSignalCodes         []string `yaml:"edit_signal_codes"`
	CommercialReplyClasses  []string `yaml:"commercial_reply_classes"`
}

var (
	playbookOnce sync.Once
	playbookInst *Playbook
	playbookErr  error
)

// LoadPlaybook loads and validates the embedded outreach playbook (singleton).
func LoadPlaybook() (*Playbook, error) {
	playbookOnce.Do(func() {
		playbookInst, playbookErr = loadPlaybookFS(playbookFS)
	})
	return playbookInst, playbookErr
}

// MustPlaybook returns the playbook or panics (tests / boot paths that require it).
func MustPlaybook() *Playbook {
	pb, err := LoadPlaybook()
	if err != nil {
		panic(err)
	}
	return pb
}

func loadPlaybookFS(fs embed.FS) (*Playbook, error) {
	pb := &Playbook{}
	if err := decodeYAML(fs, "outreach_playbook/doctrine.yaml", &pb.Doctrine); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/personas.yaml", &pb.Personas); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/services.yaml", &pb.Services); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/triggers.yaml", &pb.Triggers); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/offers.yaml", &pb.Offers); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/sequence.yaml", &pb.Sequence); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/objection_playbook.yaml", &pb.Objections); err != nil {
		return nil, err
	}
	if err := decodeYAML(fs, "outreach_playbook/copy_rules.yaml", &pb.CopyRules); err != nil {
		return nil, err
	}
	if err := ValidatePlaybook(pb); err != nil {
		return nil, err
	}
	return pb, nil
}

func decodeYAML(fs embed.FS, path string, dest any) error {
	b, err := fs.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, dest); err != nil {
		return fmt.Errorf("yaml %s: %w", path, err)
	}
	return nil
}

// ValidatePlaybook checks schema essentials (version + required libraries).
func ValidatePlaybook(pb *Playbook) error {
	if pb == nil {
		return fmt.Errorf("nil playbook")
	}
	ver := strings.TrimSpace(pb.Doctrine.OutreachDoctrineVersion)
	if ver == "" {
		return fmt.Errorf("doctrine missing outreach_doctrine_version")
	}
	if ver != OutreachDoctrineVersion {
		return fmt.Errorf("doctrine version %q != constant %q", ver, OutreachDoctrineVersion)
	}
	for _, label := range []struct {
		name string
		v    string
	}{
		{"personas", pb.Personas.OutreachDoctrineVersion},
		{"services", pb.Services.OutreachDoctrineVersion},
		{"triggers", pb.Triggers.OutreachDoctrineVersion},
		{"offers", pb.Offers.OutreachDoctrineVersion},
		{"sequence", pb.Sequence.OutreachDoctrineVersion},
		{"objections", pb.Objections.OutreachDoctrineVersion},
		{"copy_rules", pb.CopyRules.OutreachDoctrineVersion},
	} {
		if strings.TrimSpace(label.v) != ver {
			return fmt.Errorf("%s version %q != doctrine %q", label.name, label.v, ver)
		}
	}
	if len(pb.Services.Services) == 0 {
		return fmt.Errorf("services playbook empty")
	}
	if len(pb.Offers.Offers) == 0 {
		return fmt.Errorf("offers library empty")
	}
	if len(pb.Sequence.Sequence.Touches) == 0 {
		return fmt.Errorf("sequence touches empty")
	}
	offerCodes := map[string]bool{}
	for _, o := range pb.Offers.Offers {
		if o.Code == "" {
			return fmt.Errorf("offer missing code")
		}
		if strings.ToUpper(o.FulfillmentCost) != "LOW" && strings.ToUpper(o.FulfillmentCost) != "MEDIUM" && strings.ToUpper(o.FulfillmentCost) != "HIGH" {
			return fmt.Errorf("offer %s invalid fulfillment_cost %q", o.Code, o.FulfillmentCost)
		}
		offerCodes[o.Code] = true
	}
	for _, s := range pb.Services.Services {
		if s.DefaultMicroOffer != "" && !offerCodes[s.DefaultMicroOffer] {
			return fmt.Errorf("service %s default offer %s not in library", s.Code, s.DefaultMicroOffer)
		}
	}
	return nil
}

// ResolveServicePlaybook matches service_code (exact, alias, or prefix before _).
func (pb *Playbook) ResolveServicePlaybook(serviceCode string) *ServicePlaybook {
	if pb == nil {
		return nil
	}
	sc := strings.ToUpper(strings.TrimSpace(serviceCode))
	if sc == "" {
		return nil
	}
	for i := range pb.Services.Services {
		s := &pb.Services.Services[i]
		if strings.EqualFold(s.Code, sc) {
			return s
		}
		for _, a := range s.Aliases {
			if strings.EqualFold(a, sc) {
				return s
			}
		}
	}
	// Prefix: REAJUSTE_14133 → REAJUSTE
	if i := strings.Index(sc, "_"); i > 0 {
		return pb.ResolveServicePlaybook(sc[:i])
	}
	return nil
}

// ResolveTrigger matches moment/trigger codes.
func (pb *Playbook) ResolveTrigger(code string) *TriggerRule {
	if pb == nil {
		return nil
	}
	c := strings.ToUpper(strings.TrimSpace(code))
	if c == "" {
		c = "GENERIC"
	}
	for i := range pb.Triggers.Triggers {
		t := &pb.Triggers.Triggers[i]
		if strings.EqualFold(t.Code, c) {
			return t
		}
		for _, a := range t.Aliases {
			if strings.EqualFold(a, c) {
				return t
			}
		}
	}
	// Heuristic keywords in free-text codes from feeds
	low := strings.ToLower(c)
	for i := range pb.Triggers.Triggers {
		t := &pb.Triggers.Triggers[i]
		if strings.Contains(low, strings.ToLower(t.Code)) {
			return t
		}
		for _, a := range t.Aliases {
			if a != "" && strings.Contains(low, strings.ToLower(a)) {
				return t
			}
		}
	}
	for i := range pb.Triggers.Triggers {
		if strings.EqualFold(pb.Triggers.Triggers[i].Code, "GENERIC") {
			return &pb.Triggers.Triggers[i]
		}
	}
	return nil
}

// FindOffer returns a micro-offer by code.
func (pb *Playbook) FindOffer(code string) *MicroOfferDef {
	if pb == nil {
		return nil
	}
	for i := range pb.Offers.Offers {
		if strings.EqualFold(pb.Offers.Offers[i].Code, code) {
			return &pb.Offers.Offers[i]
		}
	}
	return nil
}

// MapBuyerRole maps free-text role to persona code; UNKNOWN if no evidence.
func (pb *Playbook) MapBuyerRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" || r == "contato" || r == "email genérico" || r == "email generico" {
		return "UNKNOWN"
	}
	// Generic institutional addresses must not become OWNER/DIRECTOR.
	if strings.Contains(r, "contato@") || r == "geral" || r == "administrativo genérico" {
		return "UNKNOWN"
	}
	if pb == nil {
		return "UNKNOWN"
	}
	for _, p := range pb.Personas.Roles {
		if p.Code == "UNKNOWN" {
			continue
		}
		for _, kw := range p.MatchKeywords {
			if kw != "" && strings.Contains(r, strings.ToLower(kw)) {
				return p.Code
			}
		}
	}
	return "UNKNOWN"
}

// OfferApplicable checks service match and LOW budget for cold path.
func (pb *Playbook) OfferApplicable(offer *MicroOfferDef, serviceCode string, coldPath bool) bool {
	if offer == nil {
		return false
	}
	if coldPath && strings.ToUpper(offer.FulfillmentCost) != "LOW" {
		return false
	}
	sc := strings.ToUpper(strings.TrimSpace(serviceCode))
	canon := sc
	if sp := pb.ResolveServicePlaybook(serviceCode); sp != nil {
		canon = strings.ToUpper(sp.Code)
	}
	for _, a := range offer.ApplicableServiceCodes {
		if a == "*" || strings.EqualFold(a, sc) || strings.EqualFold(a, canon) {
			return true
		}
	}
	return false
}
