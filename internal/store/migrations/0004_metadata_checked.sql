-- When the scanner last asked TMDB about this file (Unix seconds; 0 never).
-- Unpublished media still missing a poster or description is looked up again
-- once this is old enough, so files scanned before an API key was set, or
-- whose lookup failed, still get their details.
ALTER TABLE media ADD COLUMN metadata_checked_at INTEGER NOT NULL DEFAULT 0;
