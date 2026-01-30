use aws_sdk_secretsmanager::Client;

pub struct SecretsManagerClient {
    client: Client,
}

impl SecretsManagerClient {
    pub fn new(config: &aws_config::SdkConfig) -> Self {
        Self {
            client: Client::new(config),
        }
    }

    pub async fn get(
        &self,
        secret_id: &str,
    ) -> Result<String, Box<dyn std::error::Error + Send + Sync>> {
        let output = self
            .client
            .get_secret_value()
            .secret_id(secret_id)
            .send()
            .await?;

        output
            .secret_string
            .ok_or_else(|| format!("Secret {} not found", secret_id).into())
    }

    pub async fn get_optional(&self, secret_id: &str) -> Option<String> {
        self.get(secret_id).await.ok()
    }
}
