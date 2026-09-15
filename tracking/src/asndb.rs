//! Source-ASN resolution from a MaxMind GeoLite2-ASN database.
//!
//! The scanner catalogue's `asn:` entries are the only practical handle on the
//! mail-security vendors: Proofpoint, Mimecast and Cisco publish no short list
//! of scanner CIDRs the way Microsoft and Barracuda do. Until this existed
//! those entries needed an edge writing the source ASN into a header, which in
//! practice meant Cloudflare with a transform rule, so an instance behind
//! nginx, Caddy or a cloud load balancer could not use half the catalogue.
//!
//! The database is optional everywhere, the same rule the platform's
//! GeoLite2-City database follows: absent or unreadable disables ASN matching
//! with one line at boot and never fails a request.

use maxminddb::{geoip2, Reader};
use std::net::IpAddr;

/// A GeoLite2-ASN database, open for the life of the process.
pub struct AsnDb {
    reader: Reader<Vec<u8>>,
}

impl AsnDb {
    /// Opens the database at `path`, or nothing. An empty path means the
    /// operator configured none; any other failure is reported and then
    /// ignored, because a labelling refinement must never stop the service.
    ///
    /// Read into memory rather than memory-mapped: GeoLite2-ASN is around
    /// 10 MB, which is nothing next to holding a mapping over a file an
    /// operator may replace in place underneath us.
    pub fn open(path: &str) -> Option<Self> {
        let path = path.trim();
        if path.is_empty() {
            return None;
        }
        match std::fs::read(path) {
            Ok(bytes) => Self::from_bytes(bytes, path),
            Err(e) => {
                tracing::warn!("Scanner ASN database {path:?} could not be read ({e}), so asn: entries cannot match");
                None
            }
        }
    }

    /// The half of `open` that the tests reach without going through a file.
    pub fn from_bytes(bytes: Vec<u8>, name: &str) -> Option<Self> {
        let reader = match Reader::from_source(bytes) {
            Ok(reader) => reader,
            Err(e) => {
                tracing::error!(
                    "Scanner ASN database {name:?} is not a readable MaxMind database ({e})"
                );
                return None;
            }
        };
        let kind = reader.metadata().database_type.clone();
        // Every MaxMind database answers a lookup, so pointing this at the
        // GeoLite2-City file an instance already has would resolve nothing and
        // say nothing. ASN and ISP are the two that carry
        // autonomous_system_number at the top level; Enterprise carries it
        // under traits and is not read here.
        let upper = kind.to_ascii_uppercase();
        if !upper.contains("ASN") && !upper.contains("ISP") {
            tracing::error!("Scanner ASN database {name:?} is a {kind:?} database, not GeoLite2-ASN, so asn: entries cannot match");
            return None;
        }
        tracing::info!("Scanner ASN database: {name} ({kind})");
        Some(Self { reader })
    }

    /// The autonomous system number announcing `ip`, if the database has one.
    /// Decoded through the reader's own GeoLite2-ASN record type rather than a
    /// field name written out here, so the schema is the library's to get right.
    pub fn lookup(&self, ip: IpAddr) -> Option<u32> {
        self.reader
            .lookup(ip)
            .ok()?
            .decode::<geoip2::Asn>()
            .ok()??
            .autonomous_system_number
    }
}

#[cfg(test)]
pub(crate) mod fixture {
    //! A hand-built MaxMind database, so the reader wiring is tested without a
    //! vendored binary or a network fetch. Only what this service reads is
    //! written: a search tree and one `autonomous_system_number` per entry.

    use std::collections::BTreeMap;
    use std::net::IpAddr;

    /// A record is a child node, a data pointer, or nothing.
    #[derive(Clone, Copy)]
    enum Record {
        Node(u32),
        Data(u32),
        Empty,
    }

    /// Builds an IPv6 database (the shape GeoLite2-ASN ships in) carrying one
    /// ASN per network. An IPv4 network sits 96 bits down the all-zero path,
    /// which is the subtree the reader descends to for a v4 lookup.
    pub fn asn_db(database_type: &str, entries: &[(&str, u32)]) -> Vec<u8> {
        let mut nodes: Vec<[Record; 2]> = vec![[Record::Empty; 2]];
        let mut offsets: BTreeMap<u32, u32> = BTreeMap::new();
        let mut data = Vec::new();

        for (network, asn) in entries {
            let (bits, len) = parse_network(network);
            let offset = *offsets.entry(*asn).or_insert_with(|| {
                let at = data.len() as u32;
                encode_asn_record(&mut data, *asn);
                at
            });
            insert(&mut nodes, bits, len, offset);
        }

        let node_count = nodes.len() as u32;
        let mut out = Vec::new();
        for node in &nodes {
            for record in node {
                let value = match record {
                    Record::Node(i) => *i,
                    Record::Empty => node_count,
                    // A data pointer is its offset past the node count and the
                    // 16-byte data-section separator.
                    Record::Data(off) => node_count + 16 + off,
                };
                out.extend_from_slice(&value.to_be_bytes());
            }
        }
        out.extend_from_slice(&[0u8; 16]);
        out.extend_from_slice(&data);
        out.extend_from_slice(b"\xab\xcd\xefMaxMind.com");
        out.extend_from_slice(&metadata(database_type, node_count));
        out
    }

    /// Walks the tree to `len` bits, creating nodes, and hangs the record there.
    fn insert(nodes: &mut Vec<[Record; 2]>, bits: u128, len: u8, data: u32) {
        let mut node = 0usize;
        for depth in 0..len {
            let bit = ((bits >> (127 - depth)) & 1) as usize;
            if depth + 1 == len {
                nodes[node][bit] = Record::Data(data);
                return;
            }
            node = match nodes[node][bit] {
                Record::Node(next) => next as usize,
                _ => {
                    nodes.push([Record::Empty; 2]);
                    let next = nodes.len() - 1;
                    nodes[node][bit] = Record::Node(next as u32);
                    next
                }
            };
        }
    }

    /// `<network>/<prefix>` as the 128-bit tree path and its depth.
    fn parse_network(network: &str) -> (u128, u8) {
        let (addr, len) = network.split_once('/').expect("network needs a prefix");
        let len: u8 = len.parse().expect("prefix length");
        match addr.parse::<IpAddr>().expect("address") {
            IpAddr::V4(v4) => (u32::from(v4) as u128, 96 + len),
            IpAddr::V6(v6) => (u128::from(v6), len),
        }
    }

    /// `{"autonomous_system_number": <asn>}`, the one field this service reads.
    fn encode_asn_record(out: &mut Vec<u8>, asn: u32) {
        out.push(0b111_00001); // map, one entry
        encode_string(out, "autonomous_system_number");
        let bytes = asn.to_be_bytes();
        let lead = bytes.iter().position(|b| *b != 0).unwrap_or(4);
        out.push(0b110_00000 | (4 - lead) as u8); // uint32, leading zeroes dropped
        out.extend_from_slice(&bytes[lead..]);
    }

    fn encode_string(out: &mut Vec<u8>, s: &str) {
        assert!(s.len() < 29, "the short-form size field stops at 28");
        out.push(0b010_00000 | s.len() as u8);
        out.extend_from_slice(s.as_bytes());
    }

    /// The nine fields the reader deserializes, in one map.
    fn metadata(database_type: &str, node_count: u32) -> Vec<u8> {
        let mut out = Vec::new();
        out.push(0b111_00000 | 9);
        encode_string(&mut out, "binary_format_major_version");
        encode_uint(&mut out, 0b101, &2u16.to_be_bytes());
        encode_string(&mut out, "binary_format_minor_version");
        encode_uint(&mut out, 0b101, &0u16.to_be_bytes());
        encode_string(&mut out, "build_epoch");
        out.extend_from_slice(&[0b000_00000, 2]); // uint64, extended type 9, value 0
        encode_string(&mut out, "database_type");
        encode_string(&mut out, database_type);
        encode_string(&mut out, "description");
        out.push(0b111_00000); // empty map
        encode_string(&mut out, "ip_version");
        encode_uint(&mut out, 0b101, &6u16.to_be_bytes());
        encode_string(&mut out, "languages");
        out.extend_from_slice(&[0b000_00000, 4]); // array, extended type 11, empty
        encode_string(&mut out, "node_count");
        encode_uint(&mut out, 0b110, &node_count.to_be_bytes());
        encode_string(&mut out, "record_size");
        encode_uint(&mut out, 0b101, &32u16.to_be_bytes());
        out
    }

    /// An unsigned value is stored without its leading zero bytes, and the
    /// size field is how many are left.
    fn encode_uint(out: &mut Vec<u8>, kind: u8, bytes: &[u8]) {
        let lead = bytes.iter().position(|b| *b != 0).unwrap_or(bytes.len());
        out.push((kind << 5) | (bytes.len() - lead) as u8);
        out.extend_from_slice(&bytes[lead..]);
    }
}

#[cfg(test)]
mod tests {
    use super::fixture::asn_db;
    use super::*;

    fn db(database_type: &str, entries: &[(&str, u32)]) -> Option<AsnDb> {
        AsnDb::from_bytes(asn_db(database_type, entries), "fixture")
    }

    #[test]
    fn resolves_v4_and_v6_sources() {
        let db = db(
            "GeoLite2-ASN",
            &[
                ("67.231.144.0/20", 22843),
                ("103.15.44.0/22", 30031),
                ("2a02:26f0::/32", 20940),
            ],
        )
        .expect("an ASN database is accepted");
        assert_eq!(db.lookup("67.231.152.7".parse().unwrap()), Some(22843));
        assert_eq!(db.lookup("103.15.45.1".parse().unwrap()), Some(30031));
        assert_eq!(db.lookup("2a02:26f0:1::9".parse().unwrap()), Some(20940));
        // An address the database does not cover is absent, not an error.
        assert_eq!(db.lookup("203.0.113.9".parse().unwrap()), None);
        assert_eq!(db.lookup("2001:db8::1".parse().unwrap()), None);
    }

    // The City database is the one an operator already has, and it answers
    // every lookup with something. Pointed at it, nothing would ever resolve
    // and nothing would say why, which is the silence this feature exists to
    // end.
    #[test]
    fn a_database_that_is_not_an_asn_database_is_refused() {
        assert!(db("GeoLite2-City", &[("67.231.144.0/20", 22843)]).is_none());
        assert!(db("GeoIP2-ISP", &[("67.231.144.0/20", 22843)]).is_some());
    }

    #[test]
    fn an_unset_missing_or_corrupt_database_is_not_a_failure() {
        assert!(AsnDb::open("").is_none());
        assert!(AsnDb::open("   ").is_none());
        assert!(AsnDb::open("/nonexistent/GeoLite2-ASN.mmdb").is_none());
        assert!(AsnDb::from_bytes(b"not a database".to_vec(), "junk").is_none());
    }
}
