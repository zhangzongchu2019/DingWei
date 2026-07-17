-- R7: session endpoint client IP + legacy admin cleanup.
ALTER TABLE session_endpoint ADD COLUMN client_ip TEXT;

DELETE FROM routing_rule WHERE id='route-root';
DELETE FROM registered_service WHERE id='sessionhelper';
DELETE FROM service_api_key WHERE id='d88d3055' OR id LIKE 'd88d3055%';
