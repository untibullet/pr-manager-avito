-- +goose Up
-- +goose StatementBegin
ALTER TABLE team_users
    ADD CONSTRAINT fk_team_users_team_id 
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE;

ALTER TABLE team_users
    ADD CONSTRAINT fk_team_users_user_id 
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE pull_requests
    ADD CONSTRAINT fk_pull_requests_author_id 
    FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE pr_reviewers
    ADD CONSTRAINT fk_pr_reviewers_pr_id 
    FOREIGN KEY (pr_id) REFERENCES pull_requests(id) ON DELETE CASCADE;

ALTER TABLE pr_reviewers
    ADD CONSTRAINT fk_pr_reviewers_reviewer_id 
    FOREIGN KEY (reviewer_id) REFERENCES users(id) ON DELETE CASCADE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pr_reviewers DROP CONSTRAINT IF EXISTS fk_pr_reviewers_reviewer_id;
ALTER TABLE pr_reviewers DROP CONSTRAINT IF EXISTS fk_pr_reviewers_pr_id;
ALTER TABLE pull_requests DROP CONSTRAINT IF EXISTS fk_pull_requests_author_id;
ALTER TABLE team_users DROP CONSTRAINT IF EXISTS fk_team_users_user_id;
ALTER TABLE team_users DROP CONSTRAINT IF EXISTS fk_team_users_team_id;
-- +goose StatementEnd
