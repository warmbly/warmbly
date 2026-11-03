package dsn

import "testing"

func TestBlame(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       Cause
	}{
		// The address is what failed.
		{"gmail no such user", "550-5.1.1 The email account that you tried to reach does not exist. Please try double-checking the recipient's email address for typos or unnecessary spaces. https://support.google.com/mail/?p=NoSuchUser", CauseRecipientInvalid},
		{"postfix user unknown", "smtp; 550 5.1.1 <lead@example.com>: Recipient address rejected: User unknown in virtual mailbox table", CauseRecipientInvalid},
		{"bare 5.1.1", "5.1.1", CauseRecipientInvalid},
		{"no such domain", "5.1.2 smtp; 550 5.1.2 Host unknown", CauseRecipientInvalid},
		{"exchange missing mailbox under 5.4.1", "550 5.4.1 Recipient address rejected: Access denied. AS(201806281) [DB5EUR03FT012.eop-EUR03.prod.protection.outlook.com]", CauseRecipientInvalid},
		{"exchange resolver", "550 5.1.10 RESOLVER.ADR.RecipientNotFound; Recipient not found by SMTP address lookup", CauseRecipientInvalid},
		{"gmail disabled", "550 5.2.1 The email account that you tried to reach is disabled.", CauseRecipientInvalid},
		{"exim unrouteable", "550 Unrouteable address", CauseRecipientInvalid},
		{"bare recipient rejection", "550 Recipient address rejected", CauseRecipientInvalid},
		{"permanent code after a quoted deferral", "451 4.7.1 try later, then 550 5.1.1 user unknown", CauseRecipientInvalid},
		{"deferral quoted after the permanent code", "550 5.1.1 <a@b>: rejected (earlier 451 4.7.1)", CauseRecipientInvalid},

		// Something other than the address failed.
		{"recipient rejection with a policy tail", "550 5.7.1 Recipient address rejected: policy restriction", CauseReputationBlock},
		{"recipient rejection while greylisting", "450 Recipient address rejected: Greylisted", CauseTransient},
		{"sender domain does not exist", "550 5.1.0 Sender domain does not exist", CauseReputationBlock},
		{"sender address does not exist", "553 sender address does not exist", CauseReputationBlock},
		{"postfix sender verification", "550 5.7.1 <bounce@sender.example>: Sender address rejected: User unknown in local recipient table", CauseReputationBlock},
		{"postfix sender domain", "550 5.1.0 <bounce@sender.example>: Sender address rejected: Domain of sender address bounce@sender.example does not exist", CauseReputationBlock},
		{"envelope sender unknown", "553 MAIL FROM rejected: no such user", CauseReputationBlock},
		{"spam without a status", "550 Message rejected as spam", CauseReputationBlock},
		{"gmail blocked", "550-5.7.1 [[IP]] Our system has detected an unusual rate of unsolicited mail originating from your IP address. To protect our users from spam, mail sent from your IP address has been blocked.", CauseReputationBlock},
		{"microsoft spamhaus", "550 5.7.1 Service unavailable, Client host [[IP]] blocked using Spamhaus. To request removal from this list see https://www.spamhaus.org/query/ip/[IP] (AS3130)", CauseReputationBlock},
		{"microsoft ip reputation", "550 5.7.1 Unfortunately, messages from [[IP]] weren't sent. Please contact your Internet service provider", CauseReputationBlock},
		{"gmail unauthenticated", "550-5.7.26 This mail has been blocked because the sender is unauthenticated.", CauseReputationBlock},
		{"sender address refused", "550 5.1.8 <bounce@localhost>: Sender address rejected", CauseReputationBlock},
		{"message too large", "552 5.2.3 Message size exceeds fixed maximum message size", CauseReputationBlock},
		{"mailbox full", "552 5.2.2 Mailbox full", CauseMailboxFull},
		{"gmail over quota", "552-5.2.2 The email account that you tried to reach is over quota.", CauseMailboxFull},
		{"quota policy names the recipient field", "552 <lead@example.com>: Recipient address rejected: Mailbox full", CauseMailboxFull},
		{"system problem", "554 5.3.0 Other mail system problem", CauseTransient},
		{"routing loop", "554 5.4.6 Routing loop detected", CauseTransient},
		{"greylisted", "451 4.7.1 Greylisted, try again later", CauseTransient},
		{"deferred over quota", "452 4.2.2 The email account that you tried to reach is over quota", CauseTransient},

		// Nothing telling: the caller keeps its conservative default.
		{"empty", "", CauseUnknown},
		{"subject only", "Undelivered Mail Returned to Sender", CauseUnknown},
		{"bare transaction failure", "554 Transaction failed", CauseUnknown},
		{"bare 5.4.1", "5.4.1", CauseUnknown},
		{"undefined status", "5.0.0", CauseUnknown},
		{"ip address is not a status", "Delivery to [IP] failed", CauseUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Blame(tc.text); got != tc.want {
				t.Fatalf("Blame(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestAddressIsFine(t *testing.T) {
	for cause, want := range map[Cause]bool{
		CauseUnknown: false, CauseRecipientInvalid: false,
		CauseMailboxFull: true, CauseReputationBlock: true, CauseTransient: true,
	} {
		if cause.AddressIsFine() != want {
			t.Errorf("%q.AddressIsFine() = %v, want %v", cause, !want, want)
		}
	}
}
