-- 0002_telegram: password-based users, Telegram ID links, and temporary
-- per-chat login bindings for the Telegram bot.

ALTER TABLE users
  ADD COLUMN password_hash text;

CREATE TABLE user_telegram_ids (
  telegram_id bigint PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX user_telegram_ids_user_idx ON user_telegram_ids (user_id);

CREATE TABLE telegram_bindings (
  chat_id    bigint PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);

CREATE INDEX telegram_bindings_expires_idx ON telegram_bindings (expires_at);
