-- Owner links page command.
UPDATE l1_decision_rule
SET pattern = '#在线|#roster|#清单', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE id = 'l1_command_roster';
