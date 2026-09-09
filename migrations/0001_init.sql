-- 0001_init: users, sessions, and images.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username   text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
  id           text PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE images (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  prompt          text NOT NULL,
  model           text NOT NULL,
  model_name      text NOT NULL,
  image_size      text,
  aspect_ratio    text NOT NULL,
  reference_count int NOT NULL DEFAULT 0,
  content_type    text NOT NULL,
  file_path       text NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX images_user_created_idx ON images (user_id, created_at DESC);
