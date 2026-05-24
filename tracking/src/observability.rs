use tracing::{error, info};

pub fn init(env: &str) {
    if env == "prod" {
        info!("Issue reporting initialized (env=prod)");
    } else {
        info!("Issue reporting initialized (env={env}, local issue logging enabled)");
    }
}

pub fn report_issue(context: &str, details: &str) {
    error!("[issue-local][tracking] {context}: {details}");
}

pub fn report_error(context: &str, err: &(dyn std::error::Error + Send + Sync)) {
    report_issue(context, &err.to_string());
}
