ALTER TABLE request_logs ADD COLUMN upstream_model_id TEXT DEFAULT '';
ALTER TABLE request_logs ADD COLUMN upstream_remote_id TEXT DEFAULT '';
ALTER TABLE request_logs ADD COLUMN finish_reason TEXT DEFAULT '';
