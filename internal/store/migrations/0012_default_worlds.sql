-- 0012 default worlds. Only chess, medicine and space are on for every child (focus release).
-- Parents and admins can turn other worlds back on in settings. This also rewrites children saved before today.
-- (Spring: a Flyway data migration. json_set rewrites one key and keeps the rest of the settings JSON.)
UPDATE child_settings
   SET json = json_set(json, '$.enabled_worlds', json('["echecs","medecine","espace"]'));
