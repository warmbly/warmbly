//! Source-network classification for the tracking endpoints.
//!
//! Security gateways fetch the pixel and walk every link in a message with an
//! ordinary browser user agent, which is exactly what the UA rules in
//! `abuse.rs` cannot see. What gives them away is where they come from: a
//! mail-filtering network, never a person's own device.
//!
//! A match never changes the response. The pixel is still served and the click
//! is still redirected, because a scanner that is refused is a scanner that
//! reports the link as dead. Only the published event is labelled, and the
//! consumer records it as a machine open or click: kept as delivery evidence,
//! never counted as engagement.
//!
//! Two scopes, because the doubt is not symmetric. A network that only ever
//! filters mail (`Scope::All`) is a machine whatever it asks for. A network
//! that also carries a mail client's own image fetches (`Scope::Clicks`) can
//! only be judged on click tickets: Microsoft and Google both proxy external
//! images for their web mail, so treating their pixel fetches as machines
//! would zero the open rate for every recipient on them, while a person's
//! click is always their own browser talking to us directly.
//!
//! Scope is not the whole of the doubt. A source can be a scanner AND still
//! carry a person: Proofpoint Isolation and Mimecast Browser Isolation render
//! a clicked page in the vendor's own cloud, so the GET on a click ticket from
//! there may have a person on the other end of it. Those entries are marked
//! `probable`, and the label travels with that flag so the consumer widens its
//! machine window instead of condemning the event outright. A delivery-time
//! scan runs within minutes of the send; a person clicking through isolation
//! runs whenever they got to it.

use crate::asndb::AsnDb;
use axum::http::HeaderMap;
use ipnet::IpNet;
use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

/// The catalogue shipped with the service. Operators extend it with
/// TRACKING_SCANNER_NETWORKS and TRACKING_SCANNER_CLICK_NETWORKS.
const BUILTIN_CATALOGUE: &str = include_str!("../scanner-networks.txt");

/// How many database lookups may resolve nothing at all before the service
/// says so. A database that opened cleanly and covers none of the traffic
/// reaching this instance is indistinguishable from a working one until
/// somebody notices the ASN half of the catalogue never fires, which is the
/// silence the database was added to end.
const ASN_SILENCE_THRESHOLD: u64 = 500;

/// What a source may be judged on.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Scope {
    /// Every request from the source is automated.
    All,
    /// Only click tickets. The source also carries a mail client's own image
    /// fetches, which are genuine opens.
    Clicks,
}

impl Scope {
    fn covers(self, kind: Request) -> bool {
        match self {
            Scope::All => true,
            Scope::Clicks => kind == Request::Click,
        }
    }
}

/// Which endpoint is asking.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Request {
    Open,
    Click,
}

/// How much a match on this source settles.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum Certainty {
    /// The source only ever filters mail, so a match is the whole verdict.
    #[default]
    Certain,
    /// The source also carries people's own requests. A match corroborates
    /// the timing rule rather than replacing it.
    Probable,
}

struct Entry {
    scope: Scope,
    certainty: Certainty,
    label: Arc<str>,
}

/// A recognised source: what to record, and how much it settles.
#[derive(Clone, Debug)]
pub struct Match {
    pub label: Arc<str>,
    /// True for a `probable` entry. Published with the event so the consumer
    /// measures it against the wider window rather than calling it a machine
    /// on the network alone.
    pub probable: bool,
}

impl From<&Entry> for Match {
    fn from(entry: &Entry) -> Self {
        Self {
            label: entry.label.clone(),
            probable: entry.certainty == Certainty::Probable,
        }
    }
}

/// Where a request's ASN may come from. Either is enough on its own, and the
/// header wins on any request that carries one.
#[derive(Default)]
pub struct AsnSources {
    /// Header a trusted proxy sets with the source ASN (Cloudflare:
    /// ip.src.asnum).
    pub header: Option<String>,
    /// Whether any peer is trusted at all. The header is read only from a
    /// proxy the operator named, so with an empty TRACKING_TRUSTED_PROXIES a
    /// configured header can never resolve anything and does not count as a
    /// source. The database has no such dependency: it reads the address.
    pub trusted_proxies: bool,
    /// GeoLite2-ASN database, which resolves the ASN from the address itself
    /// and so needs nothing of the edge.
    pub db: Option<AsnDb>,
}

/// Matches a request's source against the known-scanner catalogue.
#[derive(Default)]
pub struct ScannerNetworks {
    nets: Vec<(IpNet, Entry)>,
    asns: HashMap<u32, Entry>,
    /// Header a trusted proxy sets with the source ASN, read only from a
    /// peer the operator named. It takes precedence over the database, so an
    /// operator who already writes it keeps the behaviour they have.
    asn_header: Option<String>,
    /// Whether that header can ever be believed, which needs a trusted proxy.
    asn_header_trusted: bool,
    /// GeoLite2-ASN database, which resolves the ASN from the address itself
    /// and so needs nothing of the edge. None when none is configured.
    asn_db: Option<AsnDb>,
    /// Entries that could not be read. Reported at startup so a typo in an
    /// operator's list is visible rather than silently doing nothing.
    skipped: usize,
    /// Database lookups attempted, and how many named an ASN. Counted so the
    /// service can report a database that resolves nothing, which is what a
    /// wrong or stale file looks like from the outside.
    asn_lookups: AtomicU64,
    asn_resolved: AtomicU64,
    /// Whether the database has already been reported on, either way. One
    /// line per process: a per-request log on the pixel path is a flood.
    asn_reported: AtomicBool,
}

impl ScannerNetworks {
    /// Builds the matcher from the shipped catalogue plus the operator's own
    /// entries. Unparseable entries are skipped with a warning rather than
    /// failing the boot: one typo in an allowlist must not take tracking down.
    pub fn new(builtins: bool, all_networks: &str, click_networks: &str, asn: AsnSources) -> Self {
        let mut s = Self {
            asn_header: asn.header.filter(|h| !h.trim().is_empty()).map(|h| {
                let mut h = h.trim().to_ascii_lowercase();
                h.retain(|c| !c.is_whitespace());
                h
            }),
            asn_header_trusted: asn.trusted_proxies,
            asn_db: asn.db,
            ..Default::default()
        };
        if builtins {
            s.load_catalogue(BUILTIN_CATALOGUE, None);
        }
        s.load_catalogue(all_networks, Some(Scope::All));
        s.load_catalogue(click_networks, Some(Scope::Clicks));
        tracing::info!(
            "Scanner catalogue: {} networks, {} ASNs, {} unreadable (asn header: {}, asn database: {})",
            s.nets.len(),
            s.asns.len(),
            s.skipped,
            match (s.asn_header.as_deref(), s.asn_header_trusted) {
                (None, _) => "none",
                (Some(name), true) => name,
                // Named but unusable, which is worth saying plainly: the
                // header is only ever read from a proxy the operator listed.
                (Some(_), false) => "set, but no trusted proxy to believe it from",
            },
            if s.asn_db.is_some() { "yes" } else { "no" }
        );
        if !s.asns.is_empty() && !s.can_resolve_asn() {
            tracing::warn!(
                "Scanner catalogue has {} ASN entries and no usable source of a request's ASN, so they match nothing. Point TRACKING_SCANNER_ASN_DB at a GeoLite2-ASN database, or set TRACKING_SCANNER_ASN_HEADER together with the TRACKING_TRUSTED_PROXIES it is only ever read from",
                s.asns.len()
            );
        }
        s
    }

    /// Reads `<source> [scope] [label]` lines, `#` to end of line a comment.
    /// `force` overrides the scope column, which is how the two operator
    /// variables get their meaning while sharing one parser with the file.
    fn load_catalogue(&mut self, text: &str, force: Option<Scope>) {
        // A comment runs to the end of its LINE, so it is stripped before the
        // line is split on commas. The other order turns every comma in a
        // sentence into two more entries, none of which parse.
        for line in text.lines() {
            let line = line.split('#').next().unwrap_or("").trim();
            for entry in line.split(',') {
                self.load_entry(entry.trim(), force);
            }
        }
    }

    /// One `<source> [scope] [label]` entry, already stripped of its comment.
    fn load_entry(&mut self, line: &str, force: Option<Scope>) {
        let mut fields = line.split_whitespace();
        let Some(source) = fields.next() else {
            return;
        };
        // An entry that names no scope gets the cautious one.
        let mut scope = force.unwrap_or(Scope::Clicks);
        // Certainty is orthogonal to scope: it says whether the source can
        // also carry a person, not which requests may be judged. It is the
        // operator's to set in either variable.
        let mut certainty = Certainty::default();
        let mut label = None;
        for field in fields {
            match field {
                // The variable an operator's entry arrived in decides its
                // scope, so a scope word there is redundant, not a label.
                "all" | "clicks" if force.is_some() => {}
                "all" => scope = Scope::All,
                "clicks" => scope = Scope::Clicks,
                "certain" => certainty = Certainty::Certain,
                "probable" => certainty = Certainty::Probable,
                other if label.is_none() => label = Some(other),
                _ => {}
            }
        }
        self.insert(
            source,
            scope,
            certainty,
            Arc::from(label.unwrap_or("scanner")),
        );
    }

    fn insert(&mut self, source: &str, scope: Scope, certainty: Certainty, label: Arc<str>) {
        if let Some(num) = source
            .strip_prefix("asn:")
            .or_else(|| source.strip_prefix("AS"))
        {
            match num.trim().parse::<u32>() {
                Ok(asn) => {
                    self.asns.insert(
                        asn,
                        Entry {
                            scope,
                            certainty,
                            label,
                        },
                    );
                }
                Err(_) => {
                    self.skipped += 1;
                    tracing::warn!("scanner catalogue: ignoring unparseable ASN {source:?}");
                }
            }
            return;
        }
        // A bare address is the /32 or /128 containing it.
        let parsed = source
            .parse::<IpNet>()
            .or_else(|_| source.parse::<IpAddr>().map(IpNet::from));
        match parsed {
            Ok(net) => self.nets.push((
                net.trunc(),
                Entry {
                    scope,
                    certainty,
                    label,
                },
            )),
            Err(_) => {
                self.skipped += 1;
                tracing::warn!("scanner catalogue: ignoring unparseable network {source:?}");
            }
        }
    }

    /// The scanner source this request came from, if the source is known and
    /// its scope covers this kind of request: what to label the event, and
    /// whether that label settles the verdict or only widens the window the
    /// consumer measures it against.
    ///
    /// `trusted_peer` is whether the socket peer is one of the configured
    /// proxies. The ASN header is read only then, for the same reason the
    /// client address is: a header anyone can set is a header that lets a
    /// caller pick its own classification. The database is read from the
    /// address instead, so it needs no such trust and no edge configuration.
    pub fn classify(
        &self,
        ip: &str,
        headers: &HeaderMap,
        trusted_peer: bool,
        kind: Request,
    ) -> Option<Match> {
        let addr = ip.parse::<IpAddr>().ok();
        if let Some(asn) = self.source_asn(addr, headers, trusted_peer) {
            if let Some(entry) = self.asns.get(&asn) {
                if entry.scope.covers(kind) {
                    return Some(entry.into());
                }
            }
        }
        let addr = addr?;
        self.nets
            .iter()
            .find(|(net, entry)| entry.scope.covers(kind) && net.contains(&addr))
            .map(|(_, entry)| entry.into())
    }

    /// Whether an ASN can be established at all. A header with no trusted
    /// proxy behind it is not a source: `header_asn` refuses every request,
    /// so the catalogue's ASN entries are as inert as if none were named, and
    /// suppressing the boot warning there hides exactly the misconfiguration
    /// it exists to report.
    fn can_resolve_asn(&self) -> bool {
        (self.asn_header.is_some() && self.asn_header_trusted) || self.asn_db.is_some()
    }

    /// The source's ASN: the trusted header where one is written, otherwise
    /// the database. A header that is absent, unbelievable or unparseable
    /// falls through rather than ending the attempt, so an edge that sets it
    /// on some requests and not others still gets the database for the rest.
    fn source_asn(
        &self,
        addr: Option<IpAddr>,
        headers: &HeaderMap,
        trusted_peer: bool,
    ) -> Option<u32> {
        // Nothing in the catalogue is keyed by ASN, so nothing can match and
        // the pixel path skips the lookup entirely. This is the shipped
        // default: every vendor ASN is commented out.
        if self.asns.is_empty() {
            return None;
        }
        if let Some(asn) = self.header_asn(headers, trusted_peer) {
            return Some(asn);
        }
        let db = self.asn_db.as_ref()?;
        let resolved = db.lookup(addr?);
        self.record_lookup(resolved);
        resolved
    }

    /// Keeps the database's hit rate, and says something once: the first ASN
    /// it ever resolves, or the point at which it is clear it will not.
    fn record_lookup(&self, resolved: Option<u32>) {
        let attempts = self.asn_lookups.fetch_add(1, Ordering::Relaxed) + 1;
        if let Some(asn) = resolved {
            if self.asn_resolved.fetch_add(1, Ordering::Relaxed) == 0
                && !self.asn_reported.swap(true, Ordering::Relaxed)
            {
                tracing::info!("Scanner ASN database resolved its first source (AS{asn})");
            }
            return;
        }
        if attempts >= ASN_SILENCE_THRESHOLD
            && self.asn_resolved.load(Ordering::Relaxed) == 0
            && !self.asn_reported.swap(true, Ordering::Relaxed)
        {
            tracing::warn!("Scanner ASN database resolved no ASN in {attempts} lookups, so asn: entries are matching nothing. Check TRACKING_SCANNER_ASN_DB points at a current GeoLite2-ASN file, and that TRACKING_TRUSTED_PROXIES is set if this service is behind a proxy");
        }
    }

    fn header_asn(&self, headers: &HeaderMap, trusted_peer: bool) -> Option<u32> {
        // A header anyone can set is a header that lets a caller pick its own
        // classification, so it counts only from a proxy the operator named.
        // The database path must never become a way around that check.
        if !trusted_peer {
            return None;
        }
        let name = self.asn_header.as_deref()?;
        let raw = headers.get(name)?.to_str().ok()?.trim();
        // Cloudflare writes the bare number; some edges prefix it with AS.
        raw.strip_prefix("AS")
            .or_else(|| raw.strip_prefix("as"))
            .unwrap_or(raw)
            .parse()
            .ok()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The two things the assertions ask of a match: which entry fired, and
    /// whether it settles the verdict on its own.
    trait Matched {
        fn label(&self) -> Option<&str>;
        fn probable(&self) -> bool;
    }

    impl Matched for Option<Match> {
        fn label(&self) -> Option<&str> {
            self.as_ref().map(|m| m.label.as_ref())
        }
        fn probable(&self) -> bool {
            self.as_ref().is_some_and(|m| m.probable)
        }
    }

    fn hdr(pairs: &[(&'static str, &'static str)]) -> HeaderMap {
        let mut h = HeaderMap::new();
        for (k, v) in pairs {
            h.insert(*k, v.parse().unwrap());
        }
        h
    }

    fn builtins() -> ScannerNetworks {
        ScannerNetworks::new(true, "", "", asn_sources(Some("cf-asn"), None))
    }

    /// An edge that writes the header AND names a trusted proxy to believe it
    /// from, which is what makes a header a source at all.
    fn asn_sources(header: Option<&str>, db: Option<AsnDb>) -> AsnSources {
        AsnSources {
            header: header.map(str::to_string),
            trusted_proxies: header.is_some(),
            db,
        }
    }

    /// A GeoLite2-ASN database holding one network per ASN named here.
    fn asn_db(entries: &[(&str, u32)]) -> Option<AsnDb> {
        AsnDb::from_bytes(
            crate::asndb::fixture::asn_db("GeoLite2-ASN", entries),
            "fixture",
        )
    }

    // Exchange Online Protection filters mail and reads none, so both the
    // pixel it fetches and the ticket it walks are machines.
    #[test]
    fn exchange_online_protection_is_a_scanner_for_both() {
        let s = builtins();
        for kind in [Request::Open, Request::Click] {
            assert_eq!(
                s.classify("40.107.1.2", &hdr(&[]), false, kind).label(),
                Some("microsoft-365-protection"),
                "{kind:?} from EOP should be labelled"
            );
        }
        assert_eq!(
            s.classify("2a01:111:f400::1", &hdr(&[]), false, Request::Open)
                .label(),
            Some("microsoft-365-protection")
        );
    }

    // A whole cloud allocation is too broad to ship on: a recipient whose
    // browser egresses through Azure or Google Cloud is inside one, and their
    // real click would be recorded as automated. The catalogue documents them
    // and leaves them commented; an operator opts in per entry.
    #[test]
    fn whole_cloud_asns_are_not_on_by_default() {
        let s = builtins();
        let h = hdr(&[("cf-asn", "8075")]);
        assert_eq!(
            s.classify("13.107.128.5", &h, true, Request::Click).label(),
            None
        );
        assert_eq!(
            s.classify("13.107.128.5", &h, true, Request::Open).label(),
            None
        );
    }

    // Opted into, an ASN is a scanner for click tickets and must never be one
    // for pixels: Outlook on the web fetches external images through
    // Microsoft's own proxy, so a genuine open arrives from there.
    #[test]
    fn an_opted_in_asn_is_clicks_only() {
        let s = ScannerNetworks::new(
            false,
            "",
            "asn:8075 microsoft",
            asn_sources(Some("cf-asn"), None),
        );
        let h = hdr(&[("cf-asn", "8075")]);
        assert_eq!(
            s.classify("13.107.128.5", &h, true, Request::Click).label(),
            Some("microsoft")
        );
        assert_eq!(
            s.classify("13.107.128.5", &h, true, Request::Open).label(),
            None
        );
    }

    // Every CIDR in the shipped catalogue must already be its own network
    // address. `insert` calls `trunc()`, so `209.222.82.9/24` would silently
    // become `209.222.82.0/24` and a typo in the host part of a block would
    // widen or shift it with nothing to show for it.
    #[test]
    fn shipped_networks_are_written_in_canonical_form() {
        for line in BUILTIN_CATALOGUE.lines() {
            let line = line.split('#').next().unwrap_or("").trim();
            let Some(source) = line.split_whitespace().next() else {
                continue;
            };
            if source.starts_with("asn:") {
                continue;
            }
            let net: IpNet = source
                .parse()
                .unwrap_or_else(|_| panic!("{source} must parse"));
            assert_eq!(
                net,
                net.trunc(),
                "{source} has host bits set; write it as {}",
                net.trunc()
            );
        }
    }

    // Barracuda's published filtering blocks are narrow, per-region and
    // documented by the vendor as its own mail tier, so they ship enabled on
    // both endpoints like the EOP ranges.
    #[test]
    fn barracuda_filtering_blocks_are_a_scanner_for_both() {
        let s = builtins();
        for kind in [Request::Open, Request::Click] {
            assert_eq!(
                s.classify("209.222.82.10", &hdr(&[]), false, kind).label(),
                Some("barracuda-egd"),
                "{kind:?} from Barracuda EGD should be labelled"
            );
        }
    }

    // Proofpoint, Mimecast and Cisco are pure mail-security networks, so they
    // ship enabled. They also run browser isolation, which renders a clicked
    // page in the vendor's own cloud with a person on the other end of it, so
    // every one of them must be marked probable: the consumer then measures
    // the event against its wider window rather than condemning it outright.
    // Marked certain, these entries would take an isolated recipient's click
    // and the automation behind it.
    #[test]
    fn vendor_asns_ship_on_and_probable() {
        let s = builtins();
        for asn in [
            "22843", "30031", "39588", "42427", "52129", "26211", "60492", "16417",
        ] {
            let h = hdr(&[("cf-asn", asn)]);
            for kind in [Request::Open, Request::Click] {
                let m = s.classify("203.0.113.9", &h, true, kind);
                assert!(m.label().is_some(), "AS{asn} should be live for {kind:?}");
                assert!(m.probable(), "AS{asn} must be probable for {kind:?}");
            }
        }
    }

    // The networks that ship enabled and CERTAIN are the mail-filtering tiers
    // themselves, where no recipient ever reads their mail. A probable mark
    // there would cost every scan that arrives past the window.
    #[test]
    fn the_filtering_tiers_ship_certain() {
        let s = builtins();
        for ip in ["40.107.1.2", "209.222.82.10", "2a01:111:f400::1"] {
            let m = s.classify(ip, &hdr(&[]), false, Request::Open);
            assert!(m.label().is_some(), "{ip} should be live");
            assert!(!m.probable(), "{ip} must settle the verdict on its own");
        }
    }

    // Certainty is the operator's to set in either variable, and says nothing
    // about which requests may be judged: the two columns are independent.
    #[test]
    fn certainty_and_scope_are_independent() {
        let s = ScannerNetworks::new(
            false,
            "203.0.113.0/24 probable pf",
            "198.51.100.0/24 mc",
            asn_sources(None, None),
        );
        let both = s.classify("203.0.113.9", &hdr(&[]), false, Request::Open);
        assert_eq!(both.label(), Some("pf"));
        assert!(both.probable());
        let clicks = s.classify("198.51.100.9", &hdr(&[]), false, Request::Click);
        assert_eq!(clicks.label(), Some("mc"));
        assert!(!clicks.probable(), "certain is the default");
        assert_eq!(
            s.classify("198.51.100.9", &hdr(&[]), false, Request::Open)
                .label(),
            None
        );
        // The keyword is consumed, so it never lands in the label.
        assert_eq!(s.skipped, 0);
    }

    // Outlook on the web is served from the Exchange Online ranges, which are
    // deliberately absent from the catalogue.
    #[test]
    fn exchange_online_itself_is_not_a_scanner() {
        let s = builtins();
        assert_eq!(
            s.classify("40.100.0.1", &hdr(&[]), false, Request::Open)
                .label(),
            None
        );
        assert_eq!(
            s.classify("52.96.0.1", &hdr(&[]), false, Request::Click)
                .label(),
            None
        );
    }

    // The ASN header is a header, so it is believed only from a proxy the
    // operator named. Otherwise any caller could pick its own label, or ask
    // not to be labelled at all.
    #[test]
    fn asn_header_is_ignored_from_an_untrusted_peer() {
        let s = ScannerNetworks::new(
            false,
            "",
            "asn:8075 microsoft",
            asn_sources(Some("cf-asn"), None),
        );
        let h = hdr(&[("cf-asn", "8075")]);
        assert_eq!(
            s.classify("203.0.113.9", &h, false, Request::Click).label(),
            None
        );
        assert_eq!(
            s.classify("203.0.113.9", &h, true, Request::Click).label(),
            Some("microsoft")
        );
    }

    // With neither a header nor a database, an ASN cannot be established at
    // all and the catalogue's ASN entries are inert.
    #[test]
    fn asn_entries_need_a_header_or_a_database() {
        let s = ScannerNetworks::new(false, "", "asn:8075 microsoft", asn_sources(None, None));
        let h = hdr(&[("cf-asn", "8075")]);
        assert_eq!(
            s.classify("203.0.113.9", &h, true, Request::Click).label(),
            None
        );
        assert!(!s.can_resolve_asn(), "the boot warning fires on this");
    }

    // The header is only ever read from a proxy the operator named, so a
    // header with no TRACKING_TRUSTED_PROXIES behind it resolves nothing and
    // is not a source. Counting it as one suppressed the boot warning in
    // exactly the misconfiguration the warning exists to report.
    #[test]
    fn a_header_with_no_trusted_proxy_is_not_a_source() {
        let s = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint",
            "",
            AsnSources {
                header: Some("cf-asn".into()),
                trusted_proxies: false,
                db: None,
            },
        );
        assert!(!s.can_resolve_asn(), "the boot warning must fire");
        // And it really cannot match, which is what makes that the truth.
        let h = hdr(&[("cf-asn", "22843")]);
        assert_eq!(
            s.classify("198.51.100.4", &h, false, Request::Click)
                .label(),
            None
        );

        // A database needs no trusted proxy, so it is a source on its own.
        let db = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint",
            "",
            AsnSources {
                header: Some("cf-asn".into()),
                trusted_proxies: false,
                db: asn_db(&[("67.231.144.0/20", 22843)]),
            },
        );
        assert!(db.can_resolve_asn());
        assert_eq!(
            db.classify("67.231.152.7", &hdr(&[]), false, Request::Click)
                .label(),
            Some("proofpoint")
        );
    }

    // The point of the database: the same entry matches with no edge
    // configuration at all, from a peer nothing trusts and with no header in
    // sight. This is every instance that is not behind Cloudflare.
    #[test]
    fn a_database_makes_asn_entries_match_with_no_header() {
        let s = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint",
            "",
            asn_sources(None, asn_db(&[("67.231.144.0/20", 22843)])),
        );
        assert!(s.can_resolve_asn(), "the boot warning must not fire");
        for kind in [Request::Open, Request::Click] {
            assert_eq!(
                s.classify("67.231.152.7", &hdr(&[]), false, kind).label(),
                Some("proofpoint"),
                "{kind:?} should resolve through the database"
            );
        }
        // An address outside every catalogued ASN is still nobody.
        assert_eq!(
            s.classify("198.51.100.4", &hdr(&[]), false, Request::Click)
                .label(),
            None
        );
    }

    // An operator who already writes the header keeps exactly the behaviour
    // they have, so adding a database can never re-label a source their edge
    // has already spoken for.
    #[test]
    fn a_trusted_header_wins_over_the_database() {
        let s = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint, asn:30031 mimecast",
            "",
            asn_sources(Some("cf-asn"), asn_db(&[("67.231.144.0/20", 22843)])),
        );
        let h = hdr(&[("cf-asn", "30031")]);
        assert_eq!(
            s.classify("67.231.152.7", &h, true, Request::Click).label(),
            Some("mimecast"),
            "the edge's answer decides where it gives one"
        );
        // From an untrusted peer the header is not an answer at all, so the
        // database still speaks. The database is a lookup on the address, so
        // it is not a way around the trust check: a caller cannot move itself
        // into another ASN by asking.
        assert_eq!(
            s.classify("67.231.152.7", &h, false, Request::Click)
                .label(),
            Some("proofpoint")
        );
        // And a trusted peer that writes nothing, or writes nonsense, falls
        // through rather than ending the attempt.
        for headers in [hdr(&[]), hdr(&[("cf-asn", "unknown")])] {
            assert_eq!(
                s.classify("67.231.152.7", &headers, true, Request::Click)
                    .label(),
                Some("proofpoint")
            );
        }
    }

    // A CIDR entry and an ASN entry can both cover a source. The ASN is the
    // more specific statement about who is asking, so it is read first, and a
    // scope that does not cover this request does not fall through to it.
    #[test]
    fn an_asn_entry_is_read_before_the_network_list() {
        let s = ScannerNetworks::new(
            false,
            "",
            "asn:15169 google",
            asn_sources(None, asn_db(&[("142.250.0.0/15", 15169)])),
        );
        assert_eq!(
            s.classify("142.250.1.1", &hdr(&[]), false, Request::Click)
                .label(),
            Some("google")
        );
        assert_eq!(
            s.classify("142.250.1.1", &hdr(&[]), false, Request::Open)
                .label(),
            None,
            "a clicks-only ASN must not label a pixel fetch"
        );
    }

    // A catalogue with nothing keyed by ASN cannot match on one, so the
    // database is never consulted and an instance that turns the builtins off
    // pays nothing per pixel.
    #[test]
    fn a_catalogue_with_no_asn_entries_never_looks_anything_up() {
        let s = ScannerNetworks::new(
            false,
            "203.0.113.0/24 pf",
            "",
            asn_sources(Some("cf-asn"), asn_db(&[("67.231.144.0/20", 22843)])),
        );
        assert!(s.asns.is_empty());
        assert_eq!(
            s.source_asn(
                Some("67.231.152.7".parse().unwrap()),
                &hdr(&[("cf-asn", "22843")]),
                true
            ),
            None
        );
        assert_eq!(s.asn_lookups.load(Ordering::Relaxed), 0);
    }

    // A database that opens cleanly and covers none of the traffic reaching
    // this instance looks exactly like a working one, so the lookups are
    // counted and the service says so once. Nothing about a request changes.
    #[test]
    fn a_database_that_resolves_nothing_is_counted_and_reported_once() {
        let s = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint",
            "",
            asn_sources(None, asn_db(&[("67.231.144.0/20", 22843)])),
        );
        for _ in 0..ASN_SILENCE_THRESHOLD {
            assert_eq!(
                s.classify("198.51.100.4", &hdr(&[]), false, Request::Click)
                    .label(),
                None
            );
        }
        assert_eq!(s.asn_lookups.load(Ordering::Relaxed), ASN_SILENCE_THRESHOLD);
        assert_eq!(s.asn_resolved.load(Ordering::Relaxed), 0);
        assert!(
            s.asn_reported.load(Ordering::Relaxed),
            "a silent database must be reported"
        );
    }

    // The positive half: the first source the database names is reported too,
    // which is the only confirmation an operator gets that the file is live.
    #[test]
    fn the_first_resolved_source_is_reported_and_then_it_stays_quiet() {
        let s = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint",
            "",
            asn_sources(None, asn_db(&[("67.231.144.0/20", 22843)])),
        );
        for _ in 0..3 {
            assert_eq!(
                s.classify("67.231.152.7", &hdr(&[]), false, Request::Click)
                    .label(),
                Some("proofpoint")
            );
        }
        assert_eq!(s.asn_lookups.load(Ordering::Relaxed), 3);
        assert_eq!(s.asn_resolved.load(Ordering::Relaxed), 3);
        assert!(s.asn_reported.load(Ordering::Relaxed));
    }

    // Counting is the database's alone. A header-resolved ASN never touches
    // the lookup counters, so a Cloudflare instance with no database is not
    // accused of running a silent one.
    #[test]
    fn a_header_resolved_asn_is_not_counted_as_a_database_lookup() {
        let s = ScannerNetworks::new(
            false,
            "asn:22843 proofpoint",
            "",
            asn_sources(Some("cf-asn"), None),
        );
        assert_eq!(
            s.classify(
                "198.51.100.4",
                &hdr(&[("cf-asn", "22843")]),
                true,
                Request::Click
            )
            .label(),
            Some("proofpoint")
        );
        assert_eq!(s.asn_lookups.load(Ordering::Relaxed), 0);
        assert!(!s.asn_reported.load(Ordering::Relaxed));
    }

    // A source with no resolvable ASN, or no address at all, falls through to
    // the network list rather than short-circuiting it.
    #[test]
    fn an_unresolvable_source_still_reaches_the_network_list() {
        let s = ScannerNetworks::new(
            true,
            "",
            "",
            asn_sources(None, asn_db(&[("67.231.144.0/20", 22843)])),
        );
        assert_eq!(
            s.classify("40.107.1.2", &hdr(&[]), false, Request::Open)
                .label(),
            Some("microsoft-365-protection")
        );
        assert_eq!(
            s.classify("not-an-address", &hdr(&[]), false, Request::Open)
                .label(),
            None
        );
    }

    #[test]
    fn operator_entries_take_their_scope_from_the_variable_they_are_in() {
        let s = ScannerNetworks::new(
            false,
            "203.0.113.0/24 proofpoint, 198.51.100.7",
            "192.0.2.0/24 acme-gateway",
            asn_sources(None, None),
        );
        assert_eq!(
            s.classify("203.0.113.9", &hdr(&[]), false, Request::Open)
                .label(),
            Some("proofpoint")
        );
        // A bare address is its own /32, and needs no label.
        assert_eq!(
            s.classify("198.51.100.7", &hdr(&[]), false, Request::Open)
                .label(),
            Some("scanner")
        );
        assert_eq!(
            s.classify("192.0.2.5", &hdr(&[]), false, Request::Click)
                .label(),
            Some("acme-gateway")
        );
        assert_eq!(
            s.classify("192.0.2.5", &hdr(&[]), false, Request::Open)
                .label(),
            None
        );
    }

    // One bad line must not take the rest of the catalogue with it, and must
    // not stop the service booting.
    #[test]
    fn unparseable_entries_are_skipped() {
        let s = ScannerNetworks::new(
            false,
            "not-a-cidr, 203.0.113.0/24 pf, asn:nope",
            "",
            asn_sources(None, None),
        );
        assert_eq!(s.skipped, 2);
        assert_eq!(
            s.classify("203.0.113.9", &hdr(&[]), false, Request::Open)
                .label(),
            Some("pf")
        );
    }

    #[test]
    fn builtins_can_be_turned_off() {
        let s = ScannerNetworks::new(false, "", "", asn_sources(Some("cf-asn"), None));
        assert!(s.nets.is_empty() && s.asns.is_empty());
        assert_eq!(
            s.classify("40.107.1.2", &hdr(&[]), false, Request::Open)
                .label(),
            None
        );
    }

    // The shipped file is read at compile time; a typo in it would silently
    // disarm the default the whole feature rests on. `skipped` also catches
    // the file's own prose being mistaken for entries, which is what happens
    // if the comment is not stripped before the line is split on commas.
    #[test]
    fn shipped_catalogue_parses_completely() {
        let s = builtins();
        let lines = BUILTIN_CATALOGUE
            .lines()
            .filter(|l| {
                let l = l.split('#').next().unwrap_or("").trim();
                !l.is_empty()
            })
            .count();
        assert_eq!(s.nets.len() + s.asns.len(), lines, "every entry loaded");
        assert_eq!(s.skipped, 0, "nothing in the shipped file was unreadable");
    }

    // An operator's list is one line of commas; the file is many lines, most
    // of them prose. Both go through the same parser.
    #[test]
    fn a_comment_is_stripped_before_the_line_is_split_on_commas() {
        let s = ScannerNetworks::new(false, "# a note, with a comma in it\n203.0.113.0/24 pf\n198.51.100.0/24 mc # trailing note, ignored", "", asn_sources(None, None));
        assert_eq!(s.skipped, 0, "prose must not be read as entries");
        assert_eq!(
            s.classify("203.0.113.1", &hdr(&[]), false, Request::Open)
                .label(),
            Some("pf")
        );
        assert_eq!(
            s.classify("198.51.100.1", &hdr(&[]), false, Request::Open)
                .label(),
            Some("mc")
        );
    }
}
