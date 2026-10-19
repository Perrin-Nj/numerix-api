# Guide d'enregistrement de voix / Voice recording guide

Numerix : jeu de maths pour enfants de 6 à 12 ans. Fichiers prêts pour l'app : voir `docs/VOICE_SOURCES.md`.
Sources de la pratique : [Such A Voice, voicing toys/games/apps](https://www.suchavoice.com/?p=15228), [Voice Crafters rate guide](https://www.voicecrafters.com/?p=140657), [Aberystwyth, home recording](https://wordpress.aber.ac.uk/e-learning/2020/10/29/vocal-warm-ups-and-techniques-for-recording-at-home/). Cibles techniques (48 kHz, 24 bits, -16 LUFS) = pratique courante de l'industrie, non tirée d'une norme unique.

---

## Français (Canada / Québec, fr-CA)

### Avant de commencer
1. Signer le formulaire de consentement (voir plus bas). La voix est un renseignement personnel (Loi 25) : sans signature, le clip n'entre pas dans l'app.
2. Parler avec son accent naturel québécois. Ne pas "faire France". Écrire dans la feuille de route la région du lecteur (ex. Québec, Estrie).
3. Une personne = une voix. Garder la même voix pour tous les nombres.

### Style de voix pour enfants de 6 à 12 ans
- Chaleureux, souriant (sourire en parlant se "entend"), comme un grand frère ou une enseignante, jamais condescendant.
- Un peu plus lent qu'une lecture adulte (environ 130 à 150 mots/minute), articulation nette, pas de monotonie.
- Phrase d'encouragement : montante et joyeuse. Mauvaise réponse : douce, jamais de reproche ("Essaie encore, tu y es presque").
- Un seul mot ou une seule phrase par prise. Pas de souffle fort, pas de "euh".
- Nombres : dire le mot seul, ton neutre-chaud, fin de mot descendante (même "intonation" pour 1 à 100, sinon les clips collés sonnent faux).

### Matériel et pièce (téléphone suffit)
- Pièce : placard rempli de vêtements, ou sous une couverture. Éviter cuisine, salle de bain (écho). Éteindre frigo, ventilateur, chauffage, notifications. Mode avion.
- Téléphone à 15 à 20 cm de la bouche, légèrement de côté (évite les "p" qui claquent). Micro vers la bouche. Ne pas le tenir en parlant (bruits de manipulation) : l'appuyer sur un livre.
- Un micro-cravate USB-C/Lightning (environ 20 à 40 $) améliore beaucoup.
- Application : Dictaphone / Voice Memos en mode sans perte ("Lossless" iOS), ou "Enregistreur" Android en WAV. Éviter les modes "enregistrement de réunion" avec réduction de bruit.
- Format de master : WAV 48 kHz, 24 bits, mono. Si le téléphone ne propose que M4A, c'est acceptable en 44,1 ou 48 kHz, jamais d'effet ni de filtre.

### Procédure
1. Faire 30 secondes de silence de la pièce ("room tone") au début de chaque session, sans bouger. Nommer `room_tone.wav`.
2. Échauffement : boire de l'eau tiède, bâiller, 2 minutes de virelangues ("les chaussettes de l'archiduchesse...").
3. Lire la liste du recorder, un élément à la fois. Marquer 1 seconde de silence avant et après chaque prise.
4. Faire 3 prises par élément. Si on se trompe, ne pas arrêter : garder le silence 2 secondes et reprendre le même élément (facile à couper).
5. Niveau : voix normale, crêtes autour de -12 dBFS sur le compteur (jamais dans le rouge). Si l'application n'affiche pas de niveau, parler à voix normale, pas plus fort.
6. Pause de 10 minutes par 30 minutes d'enregistrement. Maximum 90 minutes par session.
7. Nommer les fichiers `fr-CA_<identifiant>_<prise>.wav` (exemple `fr-CA_num_42_t2.wav`).

### Livraison (pour l'équipe, pas pour le lecteur)
- Master archivé : WAV 48 kHz 24 bits.
- Version app : AAC 64 à 96 kb/s mono 48 kHz (`.m4a`) ou Opus 64 à 96 kb/s (`.opus`).
- Volume : -16 LUFS intégré, crête vraie max -1,5 dBTP, coupure des silences en début/fin (30 ms de marge). Le script `tools/voice/fetch.mjs` applique déjà ce traitement aux clips publics ; réutiliser sa fonction `convert`.

### Consentement (Loi 25 du Québec)
Formulaire séparé, clair, daté, signé. Il doit dire : qui enregistre, pourquoi (jeu éducatif Numerix), où les clips seront utilisés (app, web, sans limite de lectures, durée illimitée), qui y aura accès, droit de retirer le consentement (les nouveaux téléchargements cesseront ; retrait limité pour ce qui est déjà publié, à préciser), et si la voix peut être modifiée (coupe, volume). Pour une personne de moins de 14 ans, le consentement du titulaire de l'autorité parentale est requis. Source : [Gouvernement du Québec, Loi 25](https://www.quebec.ca/nouvelles/actualites/details/loi-25-sur-la-protection-des-renseignements-personnels-des-citoyens-du-quebec-entree-en-vigueur-de-nouvelles-dispositions-qui-font-du-quebec-un-chef-de-file-mondial-50726). Faire valider le texte final par un juriste.

---

## English (Canada, en-CA)

### Before you start
1. Sign the consent/release form (below). A voice is personal information; no signature, no clip in the app.
2. Use your natural Canadian accent ("about", "process", "zed" for Z). Note your region on the sheet.
3. One person, one voice. Record all numbers in the same voice.

### Voice style for ages 6 to 12
- Warm and smiling (a smile is audible), like a friendly teacher or older sibling, never talking down.
- A bit slower than adult reading (about 130 to 150 words per minute), crisp consonants, not monotone.
- Praise: bright, rising energy. Wrong answer: gentle, never blaming ("Try again, you're almost there").
- One word or one sentence per take. No loud breaths, no "um".
- Numbers: say the word alone, neutral-warm, falling ending, same intonation for 1 to 100 so spliced clips sound natural.

### Gear and room (a phone is enough)
- Room: a closet full of clothes, or under a blanket. Avoid kitchens and bathrooms (echo). Switch off fridge, fan, heating, notifications. Airplane mode.
- Phone 15 to 20 cm from your mouth, slightly off-axis (reduces plosive "p" pops). Do not hold it while talking: prop it on a book.
- A USB-C/Lightning lavalier mic (about CAD 20 to 40) is a big upgrade.
- App: Voice Memos with Lossless on iOS, or a recorder that saves WAV on Android. Avoid "meeting" modes with noise reduction.
- Master format: WAV 48 kHz, 24-bit, mono. If only M4A is possible, that is acceptable; never add effects or filters.

### Procedure
1. Record 30 seconds of room tone at the start of each session, sitting still. Name it `room_tone.wav`.
2. Warm up: sip warm water, yawn, 2 minutes of tongue twisters.
3. Read the recorder tool's list one item at a time. Leave 1 second of silence before and after each take.
4. Do 3 takes per item. On a mistake, keep going: stay silent 2 seconds and redo the same item (easy to cut).
5. Level: normal speaking voice, peaks near -12 dBFS on the meter (never red). If no meter, do not speak louder than normal.
6. Rest 10 minutes per 30 minutes of recording. Max 90 minutes per session.
7. File names `en-CA_<id>_<take>.wav` (for example `en-CA_num_42_t2.wav`).

### Delivery (for the team)
- Archive master: WAV 48 kHz 24-bit.
- App build: AAC 64 to 96 kbps mono 48 kHz (`.m4a`) or Opus 64 to 96 kbps.
- Loudness: -16 LUFS integrated, true peak max -1.5 dBTP, trim leading/trailing silence (keep 30 ms). `tools/voice/fetch.mjs` `convert()` does this already.

### Consent (Quebec Law 25, also good practice in the rest of Canada)
Separate, plain, dated, signed form. It must state: who records, why (Numerix educational game), where clips run (app and web, unlimited plays, no end date), who sees them, how to withdraw consent (future builds stop using the clip; already-shipped builds may keep it, to be stated), and whether edits are allowed (cuts, level, pitch-neutral cleanup). For a person under 14, consent from the holder of parental authority is required ([Quebec.ca, Loi 25](https://www.quebec.ca/nouvelles/actualites/details/loi-25-sur-la-protection-des-renseignements-personnels-des-citoyens-du-quebec-entree-en-vigueur-de-nouvelles-dispositions-qui-font-du-quebec-un-chef-de-file-mondial-50726)). Have a lawyer validate the final text.

### Self-check before sending
- No background hum, clicks, or room echo in the first and last second.
- Same distance and volume as the first take.
- No clipping (waveform never touches the top or bottom).
- Every item recorded at least 3 times.
