-- 0015 three-worlds grade ranges. Medicine and space now serve grades 1 to 4 (they started at grade 2).
-- (Spring: a Flyway data migration; it only widens the range, so no child loses access.)
UPDATE worlds SET grade_min = 1 WHERE id IN ('medecine', 'espace') AND grade_min > 1;
UPDATE worlds SET strand = 'arithmetique,mesure,geometrie' WHERE id = 'espace' AND strand = 'arithmetique,mesure';
