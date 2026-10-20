ALTER TABLE commands ADD COLUMN actor_kind text NOT NULL DEFAULT 'unknown', ADD COLUMN actor_auth text NOT NULL DEFAULT '', ADD COLUMN actor_name text NOT NULL DEFAULT '', ADD COLUMN actor_sub text NOT NULL DEFAULT '', ADD COLUMN steps jsonb NOT NULL DEFAULT '[]', ADD COLUMN target_results jsonb NOT NULL DEFAULT '[]';
CREATE TABLE docker_stacks (device_id text NOT NULL REFERENCES devices(id) ON DELETE CASCADE, project text NOT NULL, data jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(device_id,project));
CREATE TABLE IF NOT EXISTS docker_audit (id bigserial PRIMARY KEY, device_id text NOT NULL, kind text NOT NULL, action text NOT NULL, actor_sub text NOT NULL DEFAULT '', data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS docker_audit_time ON docker_audit(created_at);

CREATE TABLE docker_inventory (device_id text PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE, data jsonb NOT NULL, collected_at timestamptz NOT NULL);

ALTER TABLE terminal_sessions DROP CONSTRAINT IF EXISTS terminal_sessions_device_id_fkey;
INSERT INTO docker_audit(device_id,kind,action,data,created_at) SELECT device_id,'terminal','open',jsonb_build_object('session_id',session_id,'container',container_id,'remote_addr',remote_addr,'ended_at',ended_at),started_at FROM terminal_sessions t WHERE NOT EXISTS(SELECT 1 FROM docker_audit a WHERE a.kind='terminal' AND a.data->>'session_id'=t.session_id);
CREATE INDEX IF NOT EXISTS docker_audit_session ON docker_audit((data->>'session_id')) WHERE kind='terminal';

ALTER TABLE events ADD COLUMN command_id text NOT NULL DEFAULT '', ADD COLUMN command_attr text NOT NULL DEFAULT '';
CREATE INDEX events_docker_observed ON events(created_at DESC,id DESC) WHERE command_id='' AND type LIKE 'container_%';

CREATE TABLE docker_stack_budget(device_id text PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,window_at timestamptz NOT NULL DEFAULT now(),writes int NOT NULL DEFAULT 0);
ALTER TABLE docker_stacks SET(autovacuum_vacuum_scale_factor=0.02,autovacuum_analyze_scale_factor=0.02);

ALTER TABLE commands ADD COLUMN result_bytes bigint NOT NULL DEFAULT 0;
ALTER TABLE command_output ADD COLUMN bytes bigint NOT NULL DEFAULT 0;
CREATE INDEX commands_result_budget ON commands(created_at) INCLUDE(result_bytes) WHERE result_bytes>0;
CREATE INDEX command_output_budget ON command_output(created_at) INCLUDE(bytes) WHERE bytes>0;
