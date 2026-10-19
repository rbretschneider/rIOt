ALTER TABLE commands DROP COLUMN result_bytes;
ALTER TABLE command_output DROP COLUMN bytes;
DROP TABLE docker_stack_budget;
ALTER TABLE events DROP COLUMN command_id,DROP COLUMN command_attr;
DROP TABLE docker_inventory;
DROP TABLE docker_stacks; ALTER TABLE commands DROP COLUMN actor_kind,DROP COLUMN actor_auth,DROP COLUMN actor_name,DROP COLUMN actor_sub,DROP COLUMN steps,DROP COLUMN target_results;
ALTER TABLE terminal_sessions ADD CONSTRAINT terminal_sessions_device_id_fkey FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE CASCADE NOT VALID;
