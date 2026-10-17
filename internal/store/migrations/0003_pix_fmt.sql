-- The video stream's pixel format, as ffprobe reports it (e.g. "yuv420p").
-- Browsers only decode 8-bit 4:2:0 H.264, so 10-bit files must be converted.
-- Empty means not probed yet; the scanner fills it in for existing H.264 files.
ALTER TABLE media ADD COLUMN pix_fmt TEXT NOT NULL DEFAULT '';
