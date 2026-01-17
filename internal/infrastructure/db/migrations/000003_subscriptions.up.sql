CREATE TABLE durations (
	id UUID NOT NULL DEFAULT gen_random_uuid(),
	title VARCHAR(100)
	PRIMARY KEY (id),
);

CREATE TABLE plans (
	id UUID NOT NULL DEFAULT gen_random_uuid(),
	max_contacts BIGINT NOT NULL,
	daily_emails INT NOT NULL,
	ai_generation BOOLEAN NOT NULL,
	account_limit BOOLEAN NOT NULL,
	price DECIMAL(10, 2) NOT NULL,
	discounted_price DECIMAL(10, 2) NOT NULL,
	duration_id UUID NOT NULL,
	savings SMALLINT ,
	public BOOLEAN NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

	PRIMARY KEY (id),
	FOREIGN KEY (duration_id) REFERENCES durations(id) ON DELETE CASCADE,
	CONSTRAINT valid_savings CHECK (savings BETWEEN 0 AND 100)
);

CREATE TABLE offers (
	id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
	title VARCHAR(255) NOT NULL,
	description TEXT NOT NULL,
	updated_at TIMESTAMP NOT NULL DEFAULT now(),
	created_at TIMESTAMP NOT NULL DEFAULT now()

	PRIMARY KEY (id)
);

CREATE TABLE offer_options (
	offer_id UUID NOT NULL,
	plan_id UUID NOT NULL,
	title VARCHAR(255) NOT NULL,

	PRIMARY KEY (offer, plan)
	FOREIGN KEY (offer_id) REFERENCES offers (id) ON DELETE CASCADE,
	FOREIGN KEY (plan_id) REFERENCES plans (id) ON DELETE CASCADE

);

CREATE TABLE secret_plans (
	user_id UUID NOT NULL,
	plan UUID NOT NULL,
	PRIMARY KEY (user_id, plan),
	FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
	FOREIGN KEY (plan_id) REFERENCES plans (id) ON DELETE CASCADE
);

CREATE TABLE subscriptions (
	id UUID NOT NULL DEFAULT gen_random_uuid(),
	user_id UUID NOT NULL,
	plan UUID NOT NULL,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL,

	PRIMARY KEY (id),
	FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
	FOREIGN KEY (plan_id) REFERENCES plans (id) ON DELETE CASCADE
);
