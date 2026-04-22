ALTER TABLE runs
ADD COLUMN integrity_issues_json TEXT NOT NULL DEFAULT '[]';
