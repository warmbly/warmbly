CREATE TYPE conversation_theme_status AS ENUM('inactive', 'active');
CREATE TYPE conversation_generation_status AS ENUM('none', 'slow', 'medium', 'fast', 'ultra');

CREATE TABLE conversation_themes (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    status conversation_theme_status NOT NULL DEFAULT 'inactive',
    generation_status conversation_generation_status NOT NULL DEFAULT 'none',

    PRIMARY KEY (id)
);

CREATE TABLE conversations (
    id UUID NOT NULL DEFAULT gen_random_uuid(),

    theme UUID NOT NULL,
    language UUID NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (id),
    FOREIGN KEY (theme) REFERENCES conversation_themes(id) ON DELETE SET NULL,
    FOREIGN KEY (language) REFERENCES languages(id) ON DELETE SET NULL
);

CREATE TABLE conversation_messages (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL,
    parent_id UUID,
    subject TEXT NOT NULL,

    PRIMARY KEY (id),
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES conversation_messages(id) ON DELETE CASCADE
);
