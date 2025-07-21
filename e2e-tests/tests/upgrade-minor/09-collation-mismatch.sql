SELECT 
  'Collection mismatch detected: current_version=' || pg_collation_actual_version(c.oid) ||
  ', db_version=' || c.collversion AS mismatch_message
FROM pg_database d
JOIN pg_collation c
  ON REPLACE(d.datcollate, '-', '') = REPLACE(c.collcollate, '-', '')
WHERE d.datname = current_database()
  AND pg_collation_actual_version(c.oid) IS DISTINCT FROM c.collversion;