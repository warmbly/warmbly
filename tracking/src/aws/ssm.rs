use aws_sdk_ssm::Client;

pub struct SsmParameterStore {
    client: Client,
}

impl SsmParameterStore {
    pub fn new(config: &aws_config::SdkConfig) -> Self {
        Self {
            client: Client::new(config),
        }
    }

    pub async fn get(&self, name: &str) -> Result<String, Box<dyn std::error::Error + Send + Sync>> {
        let output = self
            .client
            .get_parameter()
            .name(name)
            .with_decryption(true)
            .send()
            .await?;

        output
            .parameter
            .and_then(|p| p.value)
            .ok_or_else(|| format!("Parameter {} not found", name).into())
    }

    pub async fn get_optional(&self, name: &str) -> Option<String> {
        self.get(name).await.ok()
    }
}
