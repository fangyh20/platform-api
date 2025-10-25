-- Migration: Add production_url column to apps table
-- This allows storing the production domain for each app

ALTER TABLE apps ADD COLUMN IF NOT EXISTS production_url TEXT;

-- Create index for faster lookups by production URL
CREATE INDEX IF NOT EXISTS idx_apps_production_url ON apps(production_url);

-- Backfill production URLs for existing apps (optional - can be done later)
-- UPDATE apps SET production_url =
--   LOWER(REGEXP_REPLACE(REGEXP_REPLACE(name, '[^a-zA-Z0-9\s-]', '', 'g'), '\s+', '-', 'g'))
--   || '-' || SUBSTRING(id, LENGTH(id)-5, 6) || '.rapidbuild.app'
-- WHERE production_url IS NULL;
