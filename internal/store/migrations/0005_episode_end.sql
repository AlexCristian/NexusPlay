-- The last episode in a file that holds several (2 for "S01E01E02", a two-part
-- pilot); 0 for a single episode. The scanner records it from the file name,
-- and an administrator can change it with the episode number.
ALTER TABLE media ADD COLUMN episode_end INTEGER NOT NULL DEFAULT 0;
