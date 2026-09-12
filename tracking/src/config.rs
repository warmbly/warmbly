use crate::aws::{SecretsManagerClient, SsmParameterStore};
use std::env;
use tracing::info;

#[derive(Debug)]
pub enum ConfigError {
    Missing(String),
    Aws(String),
}

impl std::fmt::Display for ConfigError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ConfigError::Missing(key) => write!(f, "Config key '{}' not found", key),
            ConfigError::Aws(msg) => write!(f, "AWS error: {}", msg),
        }
    }
}

impl std::error::Error for ConfigError {}

#[derive(Clone, Debug)]
pub struct Config {
    pub env: String,
    pub host: String,
    pub port: u16,
    /// Event bus provider: "nats" (default) or "kafka".
    pub eventbus_provider: String,
    /// NATS connection URL (used when eventbus_provider != "kafka").
    pub nats_url: String,
    /// NATS subject prefix; the publish subject is `<prefix>.<kafka_topic>`.
    pub nats_subject_prefix: String,
    /// The event topic/subject name (shared by both backends). Default
    /// "tracking-events".
    pub kafka_topic: String,
    /// Wire format for published events: "json" (default) or "avro". It has to
    /// match the consumer's CODEC_PROVIDER, which is one setting covering both
    /// topics it reads, and worker envelopes cannot be Avro. So json is what a
    /// working deployment uses; avro needs a Schema Registry.
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub codec_provider: String,
    /// Kafka transport settings (only read by the kafka-feature build).
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub kafka_brokers: String,
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub kafka_sasl_username: Option<String>,
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub kafka_sasl_password: Option<String>,
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub schema_registry_url: String,
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub schema_registry_key: Option<String>,
    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub schema_registry_secret: Option<String>,
    /// Backend base URL for resolving click tickets (required), e.g.
    /// http://backend:8080 — the service calls
    /// GET {url}/api/v1/internal/tracked-links/:id at click time.
    pub backend_internal_url: String,
    /// Shared bearer token for the backend internal API (required; same
    /// INTERNAL_API_TOKEN the workers use).
    pub internal_api_token: String,
    /// Secret the source-address token is keyed with. An unkeyed hash of an
    /// IPv4 address is reversible by enumeration; a keyed one is only a
    /// stable name for one source. Defaults to the internal API token.
    pub ip_hash_key: String,
    /// Per-source request budget for both tracking endpoints (default 300/min).
    pub rate_limit_per_min: u32,
    /// Page-view ingest budget per source per minute. Lower than the pixel
    /// budget: a person does not view a page a second, a script does.
    pub pagehit_rate_limit_per_min: u32,
    /// CIDRs whose forwarded-IP headers are believed. Empty trusts nothing,
    /// so the socket peer is the client, the same rule as the backend.
    pub trusted_proxies: Vec<ipnet::IpNet>,
    /// The one header a trusted proxy sets with the client address. Only this
    /// header is read, so a client-supplied CF-Connecting-IP behind a generic
    /// proxy is ignored. For x-forwarded-for the proxy-appended last entry wins.
    pub client_ip_header: String,
    /// Whether the scanner catalogue shipped with the service is loaded.
    pub scanner_builtins: bool,
    /// Extra scanner sources (CIDR or `asn:<n>`) that never carry a person's
    /// own request, so both their pixel fetches and their clicks are machines.
    pub scanner_networks: String,
    /// Extra scanner sources judged on click tickets only, for networks that
    /// also proxy a mail client's own image fetches.
    pub scanner_click_networks: String,
    /// Header a trusted proxy sets with the source ASN (Cloudflare:
    /// ip.src.asnum). Empty, the default, disables ASN matching: an ASN is not
    /// something this service can work out on its own.
    pub scanner_asn_header: String,
    /// Where errors and panics are reported. Empty, the default, means nowhere:
    /// no backend is initialised and no host is contacted.
    pub posthog_key: String,
    /// The capture host for those events. Empty means PostHog Cloud US.
    pub posthog_host: String,
    /// The other backend, for an operator who reports to Sentry instead.
    pub sentry_dsn: String,
    /// The build events are tagged with, so a stack trace names a commit. The
    /// image sets it from the same VERSION the Go services are stamped with.
    pub release: String,
}

impl Config {
    /// Load configuration with env-first approach and optional AWS fallback.
    /// Priority: Environment variables -> AWS SSM/Secrets Manager (if AWS_CONFIG_ENABLED=true)
    pub async fn load() -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        let env_name = env::var("APP_ENV").unwrap_or_else(|_| "dev".to_string());
        let aws_enabled = env::var("AWS_CONFIG_ENABLED")
            .map(|v| v == "true")
            .unwrap_or(false);

        info!(
            "Loading configuration (env: {}, aws_enabled: {})",
            env_name, aws_enabled
        );

        // Initialize AWS clients if enabled
        let (params, secrets) = if aws_enabled {
            let aws_config = aws_config::load_defaults(aws_config::BehaviorVersion::latest()).await;
            let params = Some(SsmParameterStore::new(&aws_config));
            let secrets = Some(SecretsManagerClient::new(&aws_config));
            info!("AWS config loading enabled");
            (params, secrets)
        } else {
            info!("AWS config loading disabled, using environment variables only");
            (None, None)
        };

        // Host and port with defaults
        let host = env::var("TRACKING_HOST").unwrap_or_else(|_| "0.0.0.0".to_string());
        let port: u16 = env::var("TRACKING_PORT")
            .ok()
            .and_then(|p| p.parse().ok())
            .unwrap_or(3000);

        // Event bus provider (default NATS). NATS needs no Kafka/Schema-Registry
        // config, so those become optional below.
        let eventbus_provider =
            env::var("EVENTBUS_PROVIDER").unwrap_or_else(|_| "nats".to_string());
        let nats_url = env::var("NATS_URL").unwrap_or_else(|_| "nats://localhost:4222".to_string());
        let nats_subject_prefix =
            env::var("NATS_SUBJECT_PREFIX").unwrap_or_else(|_| "warmbly".to_string());
        info!("Event bus provider: {}", eventbus_provider);

        let codec_provider = env::var("CODEC_PROVIDER")
            .unwrap_or_else(|_| "json".to_string())
            .to_lowercase();
        info!("Event codec: {}", codec_provider);

        // Event topic/subject name (shared by both backends).
        let kafka_topic = Self::get_optional(
            "KAFKA_TRACKING_TOPIC",
            "kafka/tracking/topic",
            "tracking-events",
            &params,
        )
        .await;
        info!("Tracking topic: {}", kafka_topic);

        // Kafka transport (optional; only used when eventbus_provider == "kafka").
        let kafka_brokers = Self::get_optional(
            "KAFKA_BOOTSTRAP_SERVERS",
            "kafka/bootstrap_servers",
            "",
            &params,
        )
        .await;
        let schema_registry_url = Self::get_optional(
            "SCHEMA_REGISTRY_URL",
            "kafka/schema_registry/endpoint",
            "",
            &params,
        )
        .await;

        // Optional SASL credentials
        let kafka_sasl_username =
            Self::get_secret_optional("KAFKA_SASL_USERNAME", "kafka/sasl/username", &secrets).await;
        let kafka_sasl_password =
            Self::get_secret_optional("KAFKA_SASL_PASSWORD", "kafka/sasl/password", &secrets).await;

        if kafka_sasl_username.is_some() {
            info!("SASL authentication enabled");
        }

        // Optional Schema Registry credentials
        let schema_registry_key =
            Self::get_secret_optional("SCHEMA_REGISTRY_KEY", "kafka/schema_registry/key", &secrets)
                .await;
        let schema_registry_secret = Self::get_secret_optional(
            "SCHEMA_REGISTRY_SECRET",
            "kafka/schema_registry/secret",
            &secrets,
        )
        .await;

        if schema_registry_key.is_some() {
            info!("Schema Registry authentication enabled");
        }

        // Click-ticket resolver wiring (required): backend internal API base
        // URL + the shared internal bearer token.
        let backend_internal_url =
            Self::get_required("BACKEND_INTERNAL_URL", "backend/internal_url", &params).await?;
        let internal_api_token =
            Self::get_secret_optional("INTERNAL_API_TOKEN", "backend/internal_api_token", &secrets)
                .await
                .filter(|s| !s.is_empty())
                .ok_or_else(|| ConfigError::Missing("INTERNAL_API_TOKEN".to_string()))?;
        info!("Click-ticket resolver: {}", backend_internal_url);

        let rate_limit_per_min: u32 = env::var("TRACKING_RATE_LIMIT_PER_MIN")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(300);
        info!("Per-source rate limit: {}/min", rate_limit_per_min);

        let pagehit_rate_limit_per_min: u32 = env::var("TRACKING_PAGEHIT_RATE_LIMIT_PER_MIN")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(60);
        info!(
            "Per-source page-hit rate limit: {}/min",
            pagehit_rate_limit_per_min
        );

        let trusted_proxies =
            parse_trusted_proxies(&env::var("TRACKING_TRUSTED_PROXIES").unwrap_or_default());
        let client_ip_header = env::var("TRACKING_CLIENT_IP_HEADER")
            .ok()
            .map(|v| v.trim().to_ascii_lowercase())
            .filter(|v| !v.is_empty())
            .unwrap_or_else(|| "x-forwarded-for".to_string());
        info!(
            "Trusted proxies: {:?} (client ip header: {})",
            trusted_proxies, client_ip_header
        );

        let ip_hash_key = env::var("TRACKING_IP_HASH_KEY")
            .ok()
            .filter(|v| !v.trim().is_empty())
            .unwrap_or_else(|| internal_api_token.clone());

        let scanner_builtins = parse_bool(
            &env::var("TRACKING_SCANNER_BUILTINS").unwrap_or_default(),
            true,
        );
        let scanner_networks = env::var("TRACKING_SCANNER_NETWORKS").unwrap_or_default();
        let scanner_click_networks =
            env::var("TRACKING_SCANNER_CLICK_NETWORKS").unwrap_or_default();
        let scanner_asn_header = env::var("TRACKING_SCANNER_ASN_HEADER")
            .unwrap_or_default()
            .trim()
            .to_ascii_lowercase();

        // Error reporting. Read from the environment only: a key or a DSN in
        // SSM would make a self-host that never sets one still pay an AWS
        // lookup.
        //
        // POSTHOG_KEY also carries product analytics elsewhere in the platform,
        // so POSTHOG_ERROR_TRACKING=false keeps the key and reports nothing.
        let posthog_key = if parse_bool(
            &env::var("POSTHOG_ERROR_TRACKING").unwrap_or_default(),
            true,
        ) {
            env::var("POSTHOG_KEY")
                .unwrap_or_default()
                .trim()
                .to_string()
        } else {
            String::new()
        };
        let posthog_host = env::var("POSTHOG_HOST")
            .unwrap_or_default()
            .trim()
            .to_string();
        let sentry_dsn = env::var("SENTRY_DSN")
            .unwrap_or_default()
            .trim()
            .to_string();
        let release = env::var("WARMBLY_RELEASE")
            .ok()
            .map(|v| v.trim().to_string())
            .filter(|v| !v.is_empty())
            .unwrap_or_else(|| "dev".to_string());

        Ok(Self {
            env: env_name,
            host,
            port,
            eventbus_provider,
            codec_provider,
            nats_url,
            nats_subject_prefix,
            kafka_topic,
            kafka_brokers,
            kafka_sasl_username,
            kafka_sasl_password,
            schema_registry_url,
            schema_registry_key,
            schema_registry_secret,
            backend_internal_url,
            ip_hash_key,
            internal_api_token,
            rate_limit_per_min,
            pagehit_rate_limit_per_min,
            trusted_proxies,
            client_ip_header,
            scanner_builtins,
            scanner_networks,
            scanner_click_networks,
            scanner_asn_header,
            posthog_key,
            posthog_host,
            sentry_dsn,
            release,
        })
    }

    /// Load configuration from AWS only (legacy method for backwards compatibility)
    #[allow(dead_code)]
    pub async fn from_aws(env: &str) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        info!("Loading configuration from AWS for environment: {}", env);

        let aws_config = aws_config::load_defaults(aws_config::BehaviorVersion::latest()).await;
        let params = SsmParameterStore::new(&aws_config);
        let secrets = SecretsManagerClient::new(&aws_config);

        // Load from Parameter Store
        let kafka_brokers = params.get("kafka/bootstrap_servers").await?;
        info!("Loaded kafka/bootstrap_servers");

        let schema_registry_url = params.get("kafka/schema_registry/endpoint").await?;
        info!("Loaded kafka/schema_registry/endpoint");

        let kafka_topic = params
            .get_optional("kafka/tracking/topic")
            .await
            .unwrap_or_else(|| "tracking-events".to_string());
        info!("Kafka topic: {}", kafka_topic);

        let host = params
            .get_optional(&format!("/warmbly/{}/tracking/host", env))
            .await
            .unwrap_or_else(|| "0.0.0.0".to_string());

        let port: u16 = params
            .get_optional(&format!("/warmbly/{}/tracking/port", env))
            .await
            .unwrap_or_else(|| "3000".to_string())
            .parse()?;

        // Load from Secrets Manager
        let kafka_sasl_username = secrets.get_optional("kafka/sasl/username").await;
        let kafka_sasl_password = secrets.get_optional("kafka/sasl/password").await;
        let schema_registry_key = secrets.get_optional("kafka/schema_registry/key").await;
        let schema_registry_secret = secrets.get_optional("kafka/schema_registry/secret").await;

        if kafka_sasl_username.is_some() {
            info!("SASL authentication enabled");
        }
        if schema_registry_key.is_some() {
            info!("Schema Registry authentication enabled");
        }

        let backend_internal_url = params.get("backend/internal_url").await?;
        let internal_api_token = secrets
            .get_optional("backend/internal_api_token")
            .await
            .filter(|s| !s.is_empty())
            .ok_or_else(|| ConfigError::Missing("backend/internal_api_token".to_string()))?;

        Ok(Self {
            env: env.to_string(),
            host,
            port,
            eventbus_provider: "kafka".to_string(),
            codec_provider: "json".to_string(),
            nats_url: env::var("NATS_URL").unwrap_or_else(|_| "nats://localhost:4222".to_string()),
            nats_subject_prefix: env::var("NATS_SUBJECT_PREFIX")
                .unwrap_or_else(|_| "warmbly".to_string()),
            kafka_brokers,
            kafka_topic,
            kafka_sasl_username,
            kafka_sasl_password,
            schema_registry_url,
            schema_registry_key,
            schema_registry_secret,
            backend_internal_url,
            ip_hash_key: env::var("TRACKING_IP_HASH_KEY")
                .ok()
                .filter(|v| !v.trim().is_empty())
                .unwrap_or_else(|| internal_api_token.clone()),
            internal_api_token,
            rate_limit_per_min: 300,
            pagehit_rate_limit_per_min: 60,
            trusted_proxies: parse_trusted_proxies(
                &env::var("TRACKING_TRUSTED_PROXIES").unwrap_or_default(),
            ),
            client_ip_header: env::var("TRACKING_CLIENT_IP_HEADER")
                .unwrap_or_else(|_| "x-forwarded-for".to_string())
                .to_ascii_lowercase(),
            scanner_builtins: parse_bool(
                &env::var("TRACKING_SCANNER_BUILTINS").unwrap_or_default(),
                true,
            ),
            scanner_networks: env::var("TRACKING_SCANNER_NETWORKS").unwrap_or_default(),
            scanner_click_networks: env::var("TRACKING_SCANNER_CLICK_NETWORKS").unwrap_or_default(),
            scanner_asn_header: env::var("TRACKING_SCANNER_ASN_HEADER")
                .unwrap_or_default()
                .trim()
                .to_ascii_lowercase(),
            posthog_key: if parse_bool(
                &env::var("POSTHOG_ERROR_TRACKING").unwrap_or_default(),
                true,
            ) {
                env::var("POSTHOG_KEY")
                    .unwrap_or_default()
                    .trim()
                    .to_string()
            } else {
                String::new()
            },
            posthog_host: env::var("POSTHOG_HOST")
                .unwrap_or_default()
                .trim()
                .to_string(),
            sentry_dsn: env::var("SENTRY_DSN")
                .unwrap_or_default()
                .trim()
                .to_string(),
            release: env::var("WARMBLY_RELEASE")
                .ok()
                .map(|v| v.trim().to_string())
                .filter(|v| !v.is_empty())
                .unwrap_or_else(|| "dev".to_string()),
        })
    }

    /// Get required config value - env first, then AWS SSM if enabled
    async fn get_required(
        env_key: &str,
        aws_key: &str,
        params: &Option<SsmParameterStore>,
    ) -> Result<String, ConfigError> {
        // Check env var first
        if let Ok(val) = env::var(env_key) {
            if !val.is_empty() {
                return Ok(val);
            }
        }

        // Fall back to AWS if enabled
        if let Some(params) = params {
            match params.get(aws_key).await {
                Ok(val) => return Ok(val),
                Err(e) => {
                    return Err(ConfigError::Aws(format!(
                        "Failed to get {} from AWS: {}",
                        aws_key, e
                    )))
                }
            }
        }

        Err(ConfigError::Missing(env_key.to_string()))
    }

    /// Get optional config value with default - env first, then AWS SSM if enabled
    async fn get_optional(
        env_key: &str,
        aws_key: &str,
        default: &str,
        params: &Option<SsmParameterStore>,
    ) -> String {
        // Check env var first
        if let Ok(val) = env::var(env_key) {
            if !val.is_empty() {
                return val;
            }
        }

        // Fall back to AWS if enabled
        if let Some(params) = params {
            if let Some(val) = params.get_optional(aws_key).await {
                return val;
            }
        }

        default.to_string()
    }

    /// Get optional secret value - env first, then AWS Secrets Manager if enabled
    async fn get_secret_optional(
        env_key: &str,
        aws_key: &str,
        secrets: &Option<SecretsManagerClient>,
    ) -> Option<String> {
        // Check env var first
        if let Ok(val) = env::var(env_key) {
            if !val.is_empty() {
                return Some(val);
            }
        }

        // Fall back to AWS if enabled
        if let Some(secrets) = secrets {
            return secrets.get_optional(aws_key).await;
        }

        None
    }

    pub fn addr(&self) -> String {
        format!("{}:{}", self.host, self.port)
    }

    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub fn sasl_enabled(&self) -> bool {
        self.kafka_sasl_username.is_some() && self.kafka_sasl_password.is_some()
    }

    #[cfg_attr(not(feature = "kafka"), allow(dead_code))]
    pub fn schema_registry_auth(&self) -> Option<(String, String)> {
        match (&self.schema_registry_key, &self.schema_registry_secret) {
            (Some(key), Some(secret)) => Some((key.clone(), secret.clone())),
            _ => None,
        }
    }
}

/// Parses a comma-separated CIDR list; a bare address is a /32 or /128.
pub fn parse_trusted_proxies(raw: &str) -> Vec<ipnet::IpNet> {
    raw.split(',')
        .map(str::trim)
        .filter(|v| !v.is_empty())
        .filter_map(|v| {
            v.parse::<ipnet::IpNet>()
                .ok()
                .or_else(|| v.parse::<std::net::IpAddr>().ok().map(ipnet::IpNet::from))
        })
        .collect()
}

/// Reads an on/off variable the way the backend's own configuration registry
/// does, so the value the admin panel reports and the value this service acts
/// on can never disagree: only those words decide, anything else is the
/// default. `off` in particular reads as false there, and used to read as true
/// here.
fn parse_bool(raw: &str, default: bool) -> bool {
    match raw.trim().to_ascii_lowercase().as_str() {
        "1" | "true" | "yes" | "on" => true,
        "0" | "false" | "no" | "off" => false,
        _ => default,
    }
}

#[cfg(test)]
mod tests {
    use super::parse_bool;

    #[test]
    fn parse_bool_agrees_with_the_backend_registry() {
        for on in ["1", "true", "TRUE", "yes", "on", " On "] {
            assert!(parse_bool(on, false), "{on:?} should be true");
        }
        for off in ["0", "false", "FALSE", "no", "off", " Off "] {
            assert!(!parse_bool(off, true), "{off:?} should be false");
        }
        // Anything else, empty included, leaves the default standing rather
        // than being read as a value.
        for other in ["", "  ", "maybe", "2"] {
            assert!(parse_bool(other, true), "{other:?} should keep true");
            assert!(!parse_bool(other, false), "{other:?} should keep false");
        }
    }
}
