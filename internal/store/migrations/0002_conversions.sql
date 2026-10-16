-- Automatic conversion to browser-ready MP4. An empty status means the file
-- is either already playable or still waiting in the queue.
ALTER TABLE media ADD COLUMN convert_status TEXT NOT NULL DEFAULT '';
ALTER TABLE media ADD COLUMN convert_error TEXT NOT NULL DEFAULT '';
