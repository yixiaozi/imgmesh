ALTER TABLE devices DROP CONSTRAINT devices_platform;
ALTER TABLE devices ADD CONSTRAINT devices_platform CHECK (platform IN ('ios', 'web', 'desktop'));

ALTER TABLE events DROP CONSTRAINT events_client;
ALTER TABLE events ADD CONSTRAINT events_client CHECK (client IN ('web', 'ios', 'desktop'));

ALTER TABLE holdings DROP CONSTRAINT holdings_client;
ALTER TABLE holdings ADD CONSTRAINT holdings_client CHECK (client IN ('web', 'ios', 'desktop'));
