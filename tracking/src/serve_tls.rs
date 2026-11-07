//! Opt-in TLS listener beside the plain one, and pinned trust for the backend; unset, nothing changes.

use std::sync::OnceLock;

use axum_server::tls_rustls::RustlsConfig;

/// Where a PEM value comes from: the variable itself, or a file it names.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Pem {
    Inline(String),
    File(String),
}

impl Pem {
    fn read(&self) -> Result<Vec<u8>, String> {
        match self {
            Pem::Inline(v) => Ok(v.clone().into_bytes()),
            Pem::File(p) => std::fs::read(p).map_err(|e| format!("cannot read {p}: {e}")),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Mode {
    SelfSigned,
    Supplied { cert: Pem, key: Pem },
}

/// The TLS listener read from SERVE_TLS and its companions.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ServeTls {
    pub mode: Mode,
    pub port: u16,
}

/// The listener shape: the TLS half, if any, and whether the plain half is loopback only.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Listeners {
    pub tls: Option<ServeTls>,
    pub http_loopback_only: bool,
}

fn pem_from(get: &impl Fn(&str) -> Option<String>, name: &str) -> Result<Pem, String> {
    let inline = get(name).filter(|v| !v.trim().is_empty());
    let file = get(&format!("{name}_FILE"))
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty());
    match (inline, file) {
        (Some(v), None) => Ok(Pem::Inline(v)),
        (None, Some(p)) => Ok(Pem::File(p)),
        _ => Err(format!(
            "SERVE_TLS=supplied needs exactly one of {name} and {name}_FILE"
        )),
    }
}

impl Listeners {
    pub fn from_env(get: impl Fn(&str) -> Option<String>) -> Result<Self, String> {
        let trimmed = |k: &str| {
            get(k)
                .map(|v| v.trim().to_string())
                .filter(|v| !v.is_empty())
        };
        let http_loopback_only = match trimmed("SERVE_HTTP_LOOPBACK_ONLY").as_deref() {
            None | Some("false") | Some("0") => false,
            Some("true") | Some("1") => true,
            Some(other) => {
                return Err(format!(
                    "SERVE_HTTP_LOOPBACK_ONLY must be true or false, got {other:?}"
                ))
            }
        };
        let mode = match trimmed("SERVE_TLS")
            .map(|v| v.to_ascii_lowercase())
            .as_deref()
        {
            None | Some("off") | Some("false") => {
                if http_loopback_only {
                    return Err("SERVE_HTTP_LOOPBACK_ONLY=true needs SERVE_TLS, or nothing outside the container can reach the service".into());
                }
                return Ok(Self::default());
            }
            Some("self-signed") => Mode::SelfSigned,
            Some("supplied") => Mode::Supplied {
                cert: pem_from(&get, "SERVE_TLS_CERT")?,
                key: pem_from(&get, "SERVE_TLS_KEY")?,
            },
            Some(other) => {
                return Err(format!(
                    "SERVE_TLS must be self-signed or supplied, got {other:?}"
                ))
            }
        };
        let port = trimmed("SERVE_TLS_PORT")
            .and_then(|v| v.parse::<u16>().ok())
            .filter(|p| *p != 0)
            .ok_or("SERVE_TLS needs SERVE_TLS_PORT set to a port number")?;
        Ok(Self {
            tls: Some(ServeTls { mode, port }),
            http_loopback_only,
        })
    }

    /// The plain listener's host, moved to loopback when confined.
    pub fn plain_host<'a>(&self, host: &'a str) -> &'a str {
        if self.http_loopback_only {
            "127.0.0.1"
        } else {
            host
        }
    }
}

/// An in-memory ECDSA P-256 certificate; a load balancer does not validate its targets'.
pub fn self_signed_pem() -> Result<(String, String), String> {
    let key =
        rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).map_err(|e| e.to_string())?;
    let mut params =
        rcgen::CertificateParams::new(vec!["localhost".to_string()]).map_err(|e| e.to_string())?;
    params.extended_key_usages = vec![rcgen::ExtendedKeyUsagePurpose::ServerAuth];
    let cert = params.self_signed(&key).map_err(|e| e.to_string())?;
    Ok((cert.pem(), key.serialize_pem()))
}

impl ServeTls {
    /// The server configuration; rustls speaks nothing older than TLS 1.2.
    pub async fn rustls_config(&self) -> Result<RustlsConfig, String> {
        let (cert, key) = match &self.mode {
            Mode::SelfSigned => {
                let (c, k) = self_signed_pem()?;
                (c.into_bytes(), k.into_bytes())
            }
            Mode::Supplied { cert, key } => (cert.read()?, key.read()?),
        };
        RustlsConfig::from_pem(cert, key)
            .await
            .map_err(|_| "the TLS certificate and key are not a matching PEM pair".to_string())
    }
}

static BACKEND_ROOTS: OnceLock<Vec<reqwest::Certificate>> = OnceLock::new();

/// Reads BACKEND_INTERNAL_CA (or its _FILE), the only trust the backend's certificate is verified against.
pub fn load_backend_roots(
    get: impl Fn(&str) -> Option<String>,
) -> Result<Vec<reqwest::Certificate>, String> {
    let inline = get("BACKEND_INTERNAL_CA").filter(|v| !v.trim().is_empty());
    let file = get("BACKEND_INTERNAL_CA_FILE")
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty());
    let pem = match (inline, file) {
        (None, None) => return Ok(Vec::new()),
        (Some(v), None) => Pem::Inline(v),
        (None, Some(p)) => Pem::File(p),
        _ => return Err("set one of BACKEND_INTERNAL_CA and BACKEND_INTERNAL_CA_FILE".into()),
    };
    let certs = reqwest::Certificate::from_pem_bundle(&pem.read()?)
        .map_err(|_| "BACKEND_INTERNAL_CA holds no readable PEM certificate".to_string())?;
    if certs.is_empty() {
        return Err("BACKEND_INTERNAL_CA holds no PEM certificate".into());
    }
    Ok(certs)
}

/// Records the backend trust for every client built afterwards; call once at boot.
pub fn init_backend_roots(roots: Vec<reqwest::Certificate>) {
    let _ = BACKEND_ROOTS.set(roots);
}

/// A client builder for the backend's internal API, pinned to BACKEND_INTERNAL_CA when it is set.
pub fn backend_client() -> reqwest::ClientBuilder {
    with_roots(
        reqwest::Client::builder(),
        BACKEND_ROOTS.get().map(Vec::as_slice).unwrap_or_default(),
    )
}

fn with_roots(
    mut builder: reqwest::ClientBuilder,
    roots: &[reqwest::Certificate],
) -> reqwest::ClientBuilder {
    if roots.is_empty() {
        return builder;
    }
    builder = builder.tls_built_in_root_certs(false);
    for cert in roots {
        builder = builder.add_root_certificate(cert.clone());
    }
    builder
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::net::SocketAddr;

    fn env(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let map: HashMap<String, String> = pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        move |k: &str| map.get(k).cloned()
    }

    #[test]
    fn unset_is_plain() {
        assert_eq!(Listeners::from_env(env(&[])).unwrap(), Listeners::default());
        assert_eq!(
            Listeners::from_env(env(&[("SERVE_TLS", "off")])).unwrap(),
            Listeners::default()
        );
        assert_eq!(Listeners::default().plain_host("0.0.0.0"), "0.0.0.0");
    }

    #[test]
    fn reads_each_mode() {
        let l = Listeners::from_env(env(&[
            ("SERVE_TLS", "self-signed"),
            ("SERVE_TLS_PORT", "8443"),
            ("SERVE_HTTP_LOOPBACK_ONLY", "true"),
        ]))
        .unwrap();
        assert_eq!(
            l.tls,
            Some(ServeTls {
                mode: Mode::SelfSigned,
                port: 8443
            })
        );
        assert_eq!(l.plain_host("0.0.0.0"), "127.0.0.1");
        let l = Listeners::from_env(env(&[
            ("SERVE_TLS", "supplied"),
            ("SERVE_TLS_PORT", "443"),
            ("SERVE_TLS_CERT", "c"),
            ("SERVE_TLS_KEY_FILE", "/k"),
        ]))
        .unwrap();
        assert_eq!(
            l.tls.unwrap().mode,
            Mode::Supplied {
                cert: Pem::Inline("c".into()),
                key: Pem::File("/k".into())
            }
        );
    }

    #[test]
    fn refuses_incomplete_settings() {
        for pairs in [
            vec![("SERVE_TLS", "self-signed")],
            vec![("SERVE_TLS", "self-signed"), ("SERVE_TLS_PORT", "0")],
            vec![("SERVE_TLS", "yes"), ("SERVE_TLS_PORT", "8443")],
            vec![
                ("SERVE_TLS", "supplied"),
                ("SERVE_TLS_PORT", "8443"),
                ("SERVE_TLS_CERT", "c"),
            ],
            vec![
                ("SERVE_TLS", "supplied"),
                ("SERVE_TLS_PORT", "8443"),
                ("SERVE_TLS_CERT", "c"),
                ("SERVE_TLS_CERT_FILE", "/c"),
                ("SERVE_TLS_KEY", "k"),
            ],
            vec![("SERVE_HTTP_LOOPBACK_ONLY", "true")],
            vec![("SERVE_HTTP_LOOPBACK_ONLY", "maybe")],
        ] {
            assert!(
                Listeners::from_env(env(&pairs)).is_err(),
                "{pairs:?} was accepted"
            );
        }
    }

    #[test]
    fn backend_roots() {
        assert!(load_backend_roots(env(&[])).unwrap().is_empty());
        assert!(load_backend_roots(env(&[("BACKEND_INTERNAL_CA", "not pem")])).is_err());
        let (cert, _) = self_signed_pem().unwrap();
        assert_eq!(
            load_backend_roots(env(&[("BACKEND_INTERNAL_CA", &cert)]))
                .unwrap()
                .len(),
            1
        );
    }

    #[tokio::test]
    async fn pinned_client_reaches_a_self_signed_listener() {
        let _ = rustls::crypto::ring::default_provider().install_default();
        let (cert, key) = self_signed_pem().unwrap();
        let tls = ServeTls {
            mode: Mode::Supplied {
                cert: Pem::Inline(cert.clone()),
                key: Pem::Inline(key),
            },
            port: 1,
        };
        let config = tls.rustls_config().await.unwrap();
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let addr: SocketAddr = listener.local_addr().unwrap();
        let app = axum::Router::new().route("/health", axum::routing::get(|| async { "ok" }));
        listener.set_nonblocking(true).unwrap();
        let server = axum_server::from_tcp_rustls(listener, config).unwrap();
        tokio::spawn(server.serve(app.into_make_service()));

        let roots = load_backend_roots(env(&[("BACKEND_INTERNAL_CA", &cert)])).unwrap();
        let url = format!("https://localhost:{}/health", addr.port());
        let pinned = with_roots(reqwest::Client::builder(), &roots)
            .build()
            .unwrap();
        assert_eq!(
            pinned.get(&url).send().await.unwrap().text().await.unwrap(),
            "ok"
        );

        let (stranger, _) = self_signed_pem().unwrap();
        let other = load_backend_roots(env(&[("BACKEND_INTERNAL_CA", &stranger)])).unwrap();
        let untrusting = with_roots(reqwest::Client::builder(), &other)
            .build()
            .unwrap();
        assert!(
            untrusting.get(&url).send().await.is_err(),
            "an untrusted certificate was accepted"
        );
    }
}
