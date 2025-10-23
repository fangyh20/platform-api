-- RapidBuild Platform Database Schema

-- Apps table
CREATE TABLE IF NOT EXISTS apps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    prod_version INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Enable RLS on apps table
ALTER TABLE apps ENABLE ROW LEVEL SECURITY;

-- RLS policy: Users can only access their own apps
CREATE POLICY "Users can view their own apps"
    ON apps FOR SELECT
    USING (auth.uid() = user_id);

CREATE POLICY "Users can insert their own apps"
    ON apps FOR INSERT
    WITH CHECK (auth.uid() = user_id);

CREATE POLICY "Users can update their own apps"
    ON apps FOR UPDATE
    USING (auth.uid() = user_id);

CREATE POLICY "Users can delete their own apps"
    ON apps FOR DELETE
    USING (auth.uid() = user_id);

-- Versions table
CREATE TABLE IF NOT EXISTS versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    s3_code_path TEXT,
    vercel_url TEXT,
    vercel_deploy_id TEXT,
    build_log TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    UNIQUE(app_id, version_number)
);

-- Enable RLS on versions table
ALTER TABLE versions ENABLE ROW LEVEL SECURITY;

-- RLS policy: Users can access versions of their own apps
CREATE POLICY "Users can view versions of their own apps"
    ON versions FOR SELECT
    USING (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = versions.app_id
            AND apps.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can insert versions for their own apps"
    ON versions FOR INSERT
    WITH CHECK (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = versions.app_id
            AND apps.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can update versions of their own apps"
    ON versions FOR UPDATE
    USING (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = versions.app_id
            AND apps.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can delete versions of their own apps"
    ON versions FOR DELETE
    USING (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = versions.app_id
            AND apps.user_id = auth.uid()
        )
    );

-- Comments table
CREATE TABLE IF NOT EXISTS comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    version_id UUID REFERENCES versions(id) ON DELETE SET NULL,
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    page_path TEXT NOT NULL,
    element_path TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    submitted_at TIMESTAMP WITH TIME ZONE
);

-- Enable RLS on comments table
ALTER TABLE comments ENABLE ROW LEVEL SECURITY;

-- RLS policy: Users can access comments on their own apps
CREATE POLICY "Users can view comments on their own apps"
    ON comments FOR SELECT
    USING (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = comments.app_id
            AND apps.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can insert comments on their own apps"
    ON comments FOR INSERT
    WITH CHECK (
        auth.uid() = user_id
        AND EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = comments.app_id
            AND apps.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can update their own comments"
    ON comments FOR UPDATE
    USING (auth.uid() = user_id);

CREATE POLICY "Users can delete their own comments"
    ON comments FOR DELETE
    USING (auth.uid() = user_id);

-- Requirement files table
CREATE TABLE IF NOT EXISTS requirement_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    version_id UUID NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL,
    file_type TEXT NOT NULL,
    s3_path TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Enable RLS on requirement_files table
ALTER TABLE requirement_files ENABLE ROW LEVEL SECURITY;

-- RLS policy: Users can access requirement files for their own apps
CREATE POLICY "Users can view requirement files for their own apps"
    ON requirement_files FOR SELECT
    USING (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = requirement_files.app_id
            AND apps.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can insert requirement files for their own apps"
    ON requirement_files FOR INSERT
    WITH CHECK (
        EXISTS (
            SELECT 1 FROM apps
            WHERE apps.id = requirement_files.app_id
            AND apps.user_id = auth.uid()
        )
    );

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_apps_user_id ON apps(user_id);
CREATE INDEX IF NOT EXISTS idx_versions_app_id ON versions(app_id);
CREATE INDEX IF NOT EXISTS idx_versions_status ON versions(status);
CREATE INDEX IF NOT EXISTS idx_comments_app_id ON comments(app_id);
CREATE INDEX IF NOT EXISTS idx_comments_version_id ON comments(version_id);
CREATE INDEX IF NOT EXISTS idx_comments_user_id ON comments(user_id);
CREATE INDEX IF NOT EXISTS idx_comments_status ON comments(status);
CREATE INDEX IF NOT EXISTS idx_requirement_files_app_id ON requirement_files(app_id);
CREATE INDEX IF NOT EXISTS idx_requirement_files_version_id ON requirement_files(version_id);
