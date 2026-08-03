-- restore fixture for integration tests
CREATE TABLE sentinel_restore_fixture(id integer primary key, name text);
INSERT INTO sentinel_restore_fixture(id, name) VALUES (1, 'fixture');