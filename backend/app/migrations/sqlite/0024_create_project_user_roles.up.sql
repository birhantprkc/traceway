CREATE TABLE IF NOT EXISTS project_user_roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    role TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE (project_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_project_user_roles_user_id ON project_user_roles(user_id);
