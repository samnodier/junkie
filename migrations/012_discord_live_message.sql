-- One live status message per registered channel: posted when a run starts,
-- then edited in place at each phase change so Discord-only members watch a
-- single message tick through lobby -> focus -> break -> done.
ALTER TABLE discord_guilds ADD COLUMN IF NOT EXISTS live_message_id TEXT NOT NULL DEFAULT '';
