-- Add vercel_project_id column to apps table
ALTER TABLE apps ADD COLUMN IF NOT EXISTS vercel_project_id TEXT;

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_apps_vercel_project_id ON apps(vercel_project_id);
