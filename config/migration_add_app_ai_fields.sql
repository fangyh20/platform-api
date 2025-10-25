-- Migration: Add AI-generated fields to apps table
-- Date: 2025-10-24
-- Description: Adds display_name, logo, category, and color_scheme fields for LLM-powered app creation

-- Add new columns to apps table
ALTER TABLE apps
    ADD COLUMN IF NOT EXISTS display_name TEXT,
    ADD COLUMN IF NOT EXISTS logo TEXT,  -- S3 URI for app logo
    ADD COLUMN IF NOT EXISTS category TEXT,  -- productivity, social, ecommerce, content, dashboard, other
    ADD COLUMN IF NOT EXISTS color_scheme TEXT;  -- blue, green, purple, orange, red, teal, indigo

-- Add indexes for new searchable fields
CREATE INDEX IF NOT EXISTS idx_apps_category ON apps(category);

-- Update comment to reflect new structure
COMMENT ON COLUMN apps.display_name IS 'Human-readable app name with spaces (e.g., "Task Manager")';
COMMENT ON COLUMN apps.logo IS 'S3 URI for AI-generated app logo (512x512 PNG)';
COMMENT ON COLUMN apps.category IS 'App category: productivity, social, ecommerce, content, dashboard, other';
COMMENT ON COLUMN apps.color_scheme IS 'Primary color scheme: blue, green, purple, orange, red, teal, indigo';
