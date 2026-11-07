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

		// Real replies, as providers send them.
		{"gmail 550-5.1.1", "550-5.1.1 The email account that you tried to reach does not exist. Please try double-checking the recipient's email address for typos or unnecessary spaces. For more information, go to 550 5.1.1 https://support.google.com/mail/?p=NoSuchUser a1si123 - gsmtp", CauseRecipientInvalid},
		{"gmail unusual rate without the spam sentence", "550-5.7.1 [[IP]] Our system has detected an unusual rate of unsolicited mail originating from your IP address. 550 5.7.1 https://support.google.com/mail/?p=UnsolicitedIPError - gsmtp", CauseReputationBlock},
		{"gmail dmarc", "550-5.7.26 Unauthenticated email from example.com is not accepted due to domain's DMARC policy. Please contact the administrator of example.com domain.", CauseReputationBlock},
		{"outlook 5.5.0 mailbox unavailable", "550 5.5.0 Requested action not taken: mailbox unavailable (S2017062302). [AM4PEPF00025F98.EURPRD83.prod.outlook.com]", CauseRecipientInvalid},
		{"outlook 5.7.708", "550 5.7.708 Service unavailable. Access denied, traffic not accepted from this IP. For more information please go to http://go.microsoft.com/fwlink/?LinkId=526653 AS(830)", CauseReputationBlock},
		{"outlook 5.7.511 banned", "550 5.7.511 Access denied, banned sender[[IP]]. To request removal from this list please forward this message to [EMAIL].", CauseReputationBlock},
		{"spamhaus zen", "554 5.7.1 Service unavailable; Client host [[IP]] blocked using zen.spamhaus.org; https://www.spamhaus.org/query/ip/[IP]", CauseReputationBlock},
		{"yahoo 5.7.9 policy", "554 5.7.9 Message not accepted for policy reasons. See https://senders.yahooinc.com/error-codes", CauseReputationBlock},
		{"quota 5.2.2", "552 5.2.2 <[EMAIL]>: user is over quota", CauseMailboxFull},
		{"greylist 4.2.1", "450 4.2.1 The user you are trying to contact is receiving mail at a rate that prevents additional messages from being delivered. Please resend your message at a later time.", CauseTransient},
		{"bare mailbox unavailable", "550 Mailbox unavailable", CauseRecipientInvalid},
		{"policy code beats weak mailbox wording", "550 5.7.1 mailbox unavailable", CauseReputationBlock},
		{"bare user unknown", "550 User unknown", CauseRecipientInvalid},
		{"proofpoint recipient policy", "554 5.7.1 rejected by recipient policy", CauseReputationBlock},
		{"mimecast anti-spoofing", "550 Rejected by header based Anti-Spoofing policy: [EMAIL] - https://community.mimecast.com/docs/DOC-1369#550", CauseReputationBlock},
		{"bare blocked without a code", "550 Blocked", CauseReputationBlock},
		{"bare message rejected", "554 Message rejected", CauseReputationBlock},
		{"listed on a blocklist", "550 Your IP is listed on bl.example.net", CauseReputationBlock},
		{"yahoo no such account", "554 delivery error: dd This user doesn't have a yahoo.com account ([EMAIL]) [0] - mta1234.mail.bf1.yahoo.com", CauseRecipientInvalid},
		{"system status beats weak recipient wording", "554 5.3.4 Recipient address rejected: message too big for system", CauseTransient},

		// Nothing telling: the caller keeps its conservative default.
		{"empty", "", CauseUnknown},
		{"subject only", "Undelivered Mail Returned to Sender", CauseUnknown},
		{"bare transaction failure", "554 Transaction failed", CauseUnknown},
		{"bare 5.4.1", "5.4.1", CauseUnknown},
		{"bare 550", "550", CauseUnknown},
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
