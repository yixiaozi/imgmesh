CREATE TABLE usage_totals (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    photos_transferred bigint NOT NULL DEFAULT 0
);

INSERT INTO usage_totals (singleton, photos_transferred)
SELECT true, count(*)
FROM objects
WHERE kind = 'original' AND status = 'ready';
