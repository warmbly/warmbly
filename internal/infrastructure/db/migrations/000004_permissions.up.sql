CREATE TABLE roles (
	id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
	permissions BIGINT NOT NULL,
	name VARCHAR(255) NOT NULL,
	color VARCHAR(7) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT now(),
	updated_at TIMESTAMP NOT NULL DEFAULT now(),
	CONSTRAINT valid_color CHECK (color ~* '^#[a-f0-9]{6}$')
);

CREATE TABLE user_roles (
	user_id UUID NOT NULL,
	role_id UUID NOT NULL,
	PRIMARY KEY (user_id, role_id),
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
	FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);
