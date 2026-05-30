package seed

import (
	"fmt"

	"github.com/google/uuid"
)

// Stable UUIDs for every seeded entity. Using fixed IDs makes the seed truly
// idempotent (ON CONFLICT DO NOTHING needs a predictable conflict target) and
// makes data easy to reference from manual tests.
//
// Convention: zero-padded namespace prefixes per domain.

var (
	// Plans (00000000-0000-0000-0000-0000000001xx).
	// PlanFreeTrialID is the well-known UUID already inserted by migration 000015.
	PlanFreeTrialID  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	PlanStarterID    = uuid.MustParse("00000000-0000-0000-0000-000000000110")
	PlanProMonthlyID = uuid.MustParse("00000000-0000-0000-0000-000000000120")
	PlanProYearlyID  = uuid.MustParse("00000000-0000-0000-0000-000000000121")
	PlanEnterpriseID = uuid.MustParse("00000000-0000-0000-0000-000000000130")

	// Workers (00000000-0000-0000-0000-0000000002xx).
	WorkerFreeID      = uuid.MustParse("00000000-0000-0000-0000-000000000201")
	WorkerSharedID    = uuid.MustParse("00000000-0000-0000-0000-000000000202")
	WorkerDedicatedID = uuid.MustParse("00000000-0000-0000-0000-000000000203")

	// Users (00000000-0000-0000-0000-0000000003xx).
	UserAdminID   = uuid.MustParse("00000000-0000-0000-0000-000000000301")
	UserOwnerID   = uuid.MustParse("00000000-0000-0000-0000-000000000302")
	UserFounderID = uuid.MustParse("00000000-0000-0000-0000-000000000303")
	UserManagerID = uuid.MustParse("00000000-0000-0000-0000-000000000304")
	UserViewerID  = uuid.MustParse("00000000-0000-0000-0000-000000000305")

	// Organisations (00000000-0000-0000-0000-0000000004xx).
	OrgAcmeID   = uuid.MustParse("00000000-0000-0000-0000-000000000401")
	OrgGlobexID = uuid.MustParse("00000000-0000-0000-0000-000000000402")

	// Subscriptions (00000000-0000-0000-0000-0000000005xx).
	SubAcmeID   = uuid.MustParse("00000000-0000-0000-0000-000000000501")
	SubGlobexID = uuid.MustParse("00000000-0000-0000-0000-000000000502")

	// Email accounts (00000000-0000-0000-0000-0000000006xx).
	EmailAcmeAliceID  = uuid.MustParse("00000000-0000-0000-0000-000000000601")
	EmailAcmeBobID    = uuid.MustParse("00000000-0000-0000-0000-000000000602")
	EmailGlobexHansID = uuid.MustParse("00000000-0000-0000-0000-000000000603")
	EmailOwnerSelfID  = uuid.MustParse("00000000-0000-0000-0000-000000000604")

	// Campaigns (00000000-0000-0000-0000-0000000007xx).
	CampaignAcmeActiveID = uuid.MustParse("00000000-0000-0000-0000-000000000701")
	CampaignAcmeDraftID  = uuid.MustParse("00000000-0000-0000-0000-000000000702")
	CampaignGlobexID     = uuid.MustParse("00000000-0000-0000-0000-000000000703")

	// Sequences (00000000-0000-0000-0000-0000000008xx).
	SequenceAcmeStep1ID = uuid.MustParse("00000000-0000-0000-0000-000000000801")
	SequenceAcmeStep2ID = uuid.MustParse("00000000-0000-0000-0000-000000000802")
	SequenceAcmeStep3ID = uuid.MustParse("00000000-0000-0000-0000-000000000803")
	SequenceDraftID     = uuid.MustParse("00000000-0000-0000-0000-000000000804")
	SequenceGlobexID    = uuid.MustParse("00000000-0000-0000-0000-000000000805")

	// API key (00000000-0000-0000-0000-0000000009xx).
	APIKeyAcmeID = uuid.MustParse("00000000-0000-0000-0000-000000000901")

	// Folders / Tags / Categories (00000000-0000-0000-0000-000000000Axx).
	FolderInboxID   = uuid.MustParse("00000000-0000-0000-0000-000000000a01")
	FolderClosedID  = uuid.MustParse("00000000-0000-0000-0000-000000000a02")
	TagVIPID        = uuid.MustParse("00000000-0000-0000-0000-000000000a03")
	TagColdID       = uuid.MustParse("00000000-0000-0000-0000-000000000a04")
	CategoryLeadID  = uuid.MustParse("00000000-0000-0000-0000-000000000a05")
	CategoryChurnID = uuid.MustParse("00000000-0000-0000-0000-000000000a06")

	// CRM (00000000-0000-0000-0000-000000000Bxx).
	PipelineSalesID = uuid.MustParse("00000000-0000-0000-0000-000000000b01")
	StageNewID      = uuid.MustParse("00000000-0000-0000-0000-000000000b02")
	StageQualID     = uuid.MustParse("00000000-0000-0000-0000-000000000b03")
	StageDemoID     = uuid.MustParse("00000000-0000-0000-0000-000000000b04")
	StageWonID      = uuid.MustParse("00000000-0000-0000-0000-000000000b05")
	StageLostID     = uuid.MustParse("00000000-0000-0000-0000-000000000b06")
	DealAcmeBigID   = uuid.MustParse("00000000-0000-0000-0000-000000000b07")
	DealAcmeWonID   = uuid.MustParse("00000000-0000-0000-0000-000000000b08")
	CRMTask1ID      = uuid.MustParse("00000000-0000-0000-0000-000000000b09")
	CRMTask2ID      = uuid.MustParse("00000000-0000-0000-0000-000000000b0a")

	// Reply templates (00000000-0000-0000-0000-000000000Cxx).
	ReplyTemplateYesID = uuid.MustParse("00000000-0000-0000-0000-000000000c01")
	ReplyTemplateNoID  = uuid.MustParse("00000000-0000-0000-0000-000000000c02")

	// Enterprise inquiry sample.
	EnterpriseInquiryID = uuid.MustParse("00000000-0000-0000-0000-000000000d01")

	// Discount codes (00000000-0000-0000-0000-000000000Fxx).
	DiscountWelcome10ID = uuid.MustParse("00000000-0000-0000-0000-000000000f01")

	// Unibox emails (00000000-0000-0000-0000-0000000010xx).
	UniboxAcmeReplyID      = uuid.MustParse("00000000-0000-0000-0000-000000001001")
	UniboxAcmeFollowupID   = uuid.MustParse("00000000-0000-0000-0000-000000001002")
	UniboxAcmeBounceID     = uuid.MustParse("00000000-0000-0000-0000-000000001003")
	UniboxAcmeOOOID        = uuid.MustParse("00000000-0000-0000-0000-000000001004")
	UniboxAcmeMeetingID    = uuid.MustParse("00000000-0000-0000-0000-000000001005")
	UniboxAcmeVendorID     = uuid.MustParse("00000000-0000-0000-0000-000000001006")
	UniboxGlobexReplyID    = uuid.MustParse("00000000-0000-0000-0000-000000001007")
	UniboxGlobexQuestionID = uuid.MustParse("00000000-0000-0000-0000-000000001008")
	UniboxOwnerWelcomeID   = uuid.MustParse("00000000-0000-0000-0000-000000001009")
	UniboxOwnerDigestID    = uuid.MustParse("00000000-0000-0000-0000-00000000100a")
)

// contactID derives a stable per-org contact UUID from a small sequence number.
func contactID(orgPrefix byte, n int) uuid.UUID {
	return uuid.MustParse(
		fmt.Sprintf("00000000-0000-0000-0000-0000000e%02x%02x", orgPrefix, n),
	)
}

// duration UUIDs match migration 000015's seeded rows.
var (
	DurationMonthID    = uuid.MustParse("00000000-0000-0000-0000-0000000000d1")
	DurationYearID     = uuid.MustParse("00000000-0000-0000-0000-0000000000d2")
	DurationLifetimeID = uuid.MustParse("00000000-0000-0000-0000-0000000000d3")
)
