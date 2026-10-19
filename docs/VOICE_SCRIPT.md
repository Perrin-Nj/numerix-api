# Voice script for the voice actor

Owl Numa recording script. Lines marked "x2" are recorded twice: once running ("cont", as if more follows) and once sentence-final ("fin", falling). Question prompts (class e) are not printed here: they are exported from `seed/voice/script.json` (filter `class == "e"`, by priority). No timers, no pressure, no sighs.

Direction codes:

- W: warm, smiling, light and unhurried
- B: bright, genuinely pleased, smile in the voice, never shouty
- G: gentle, encouraging, never disappointed, no sigh, no drop in energy
- Q: curious, a little playful, wonder rising on the key word
- H: calm and helpful, like leaning in to share a tip
- S: soft storyteller, unhurried, small smile
- P: clear, friendly, steady pace for a 6 to 8 year old, no pressure; lift only on the final question
- N: even and clear, medium pitch, no emphasis, flat-neutral so it joins other clips

---

# Français (fr-CA)

Numa est une chouette chaleureuse, curieuse et douce, avec une pointe de jeu. Voix naturelle de Québécoise ou Québécois, sourire audible, débit tranquille, comme on parle à un enfant de 6 à 9 ans que l’on aime bien. Jamais de déception, jamais de soupir, jamais de pression : une réponse fausse est un détour, pas un échec. Accent québécois naturel, sans exagération. Ne dis pas les mots entre crochets. Reste la même personne d’une séance à l’autre : même micro, même distance, même énergie.

## fr-CA session 1 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 1 | a.welcome.01 | Salut, contente de te revoir ! | W | 3.5 | [ ] |
| 2 | a.welcome.02 | Ça te dit, une nouvelle histoire ? | W | 4 | [ ] |
| 3 | a.welcome.03 | Te voilà ! J’avais gardé ta place. | W | 4 | [ ] |
| 4 | a.welcome.04 | Bonjour ! L’aventure t’attendait. | W | 2.5 | [ ] |
| 5 | a.welcome.05 | Installe-toi, on a plein de choses à découvrir. | W | 4.5 | [ ] |
| 6 | a.welcome.06 | Ravie que tu sois là. | W | 3 | [ ] |
| 7 | a.think.01 | Hmm, voyons ça ensemble. | H | 2.5 | [ ] |
| 8 | a.think.02 | Prends ton temps, je réfléchis avec toi. | H | 4 | [ ] |
| 9 | a.think.03 | Bonne question ! On la tourne dans tous les sens ? | H | 5.5 | [ ] |
| 10 | a.think.04 | Et si on faisait des petits groupes ? | H | 4.5 | [ ] |
| 11 | a.think.05 | Doucement, rien ne presse ici. | H | 3 | [ ] |
| 12 | a.think.06 | Dessine-le dans ta tête, ça aide parfois. | H | 4 | [ ] |
| 13 | a.curious.01 | Oh, qu’est-ce qui se cache par là ? | Q | 4.5 | [ ] |
| 14 | a.curious.02 | Ça sent l’aventure, non ? | Q | 3 | [ ] |
| 15 | a.curious.03 | Je me demande ce que tu vas choisir. | Q | 4.5 | [ ] |
| 16 | a.curious.04 | Mes plumes frétillent, il y a un secret ! | Q | 5 | [ ] |
| 17 | a.curious.05 | Et si on regardait de plus près ? | Q | 4.5 | [ ] |
| 18 | a.amazed.01 | Oh là là, quel tour inattendu ! | Q | 4 | [ ] |
| 19 | a.amazed.02 | Tu as vu ça ? J’en reste le bec ouvert ! | Q | 5.5 | [ ] |
| 20 | a.amazed.03 | Incroyable, l’histoire vient de rebondir ! | Q | 3.5 | [ ] |
| 21 | a.amazed.04 | Wow, je ne m’y attendais vraiment pas. | Q | 4 | [ ] |
| 22 | a.amazed.05 | Même moi, je n’avais rien deviné ! | Q | 4 | [ ] |
| 23 | a.exit.01 | Ton histoire t’attend, reviens quand tu veux. | W | 4 | [ ] |
| 24 | a.exit.02 | À bientôt ! Je garde ta place au chaud. | W | 5 | [ ] |
| 25 | a.exit.03 | Repose-toi bien, la suite sera là demain. | W | 4 | [ ] |
| 26 | a.exit.04 | Belle aventure aujourd’hui, à très vite ! | W | 4 | [ ] |
| 27 | a.exit.05 | Merci d’avoir joué avec moi. | W | 3 | [ ] |
| 28 | a.exit.06 | Fais de beaux rêves de chiffres et d’étoiles. | W | 4.5 | [ ] |
| 29 | a.ui.sample | Bonjour ! Je suis Numa. On y va ? | W | 5 | [ ] |
| 30 | a.ui.name | Moi, c’est Numa la chouette. | W | 3 | [ ] |

## fr-CA session 2 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 31 | a.ui.play | Touche ici pour jouer. | W | 2.5 | [ ] |
| 32 | a.ui.pickworld | Choisis ton aventure. | W | 2.5 | [ ] |
| 33 | a.ui.gift | Touche le cadeau pour l’ouvrir. | W | 3 | [ ] |
| 34 | a.ui.loading | L’histoire arrive. | W | 2 | [ ] |
| 35 | a.ui.slow | L’histoire prend un petit moment. On attend ensemble. | W | 4.5 | [ ] |
| 36 | a.ui.fallback | Cette partie de l’histoire est racontée en version courte. | W | 5 | [ ] |
| 37 | a.ui.askhint | Tu veux un petit indice ? Touche le bouton. | W | 5 | [ ] |
| 38 | a.ui.listen | Touche pour m’écouter encore. | W | 2.5 | [ ] |
| 39 | a.ui.scroll | Fais défiler pour découvrir. | W | 2.5 | [ ] |
| 40 | a.ui.cards | Voici ta collection de cartes. | W | 3 | [ ] |
| 41 | a.ui.progress | Voici ta série et ta progression. | W | 3.5 | [ ] |
| 42 | a.ui.back | On revient en arrière ? | W | 3 | [ ] |
| 43 | a.ui.next | On continue ? | W | 2.5 | [ ] |
| 44 | a.ui.choose | Choisis une réponse. | W | 2.5 | [ ] |
| 45 | a.ui.type | Écris ta réponse avec les touches. | W | 3.5 | [ ] |
| 46 | a.ui.noclock | Ici, pas de chrono. Prends tout ton temps. | W | 4.5 | [ ] |
| 47 | a.ui.sleepy | Une petite pause ? Je serai là quand tu reviendras. | W | 5.5 | [ ] |
| 48 | b.correct.01 | Bravo, bien vu ! | B | 2.5 | [ ] |
| 49 | b.correct.02 | Oui, c’est ça ! | B | 2.5 | [ ] |
| 50 | b.correct.03 | Tout juste ! | B | 2.5 | [ ] |
| 51 | b.correct.04 | Super, tu l’as trouvé ! | B | 3 | [ ] |
| 52 | b.correct.05 | Bien joué ! | B | 2.5 | [ ] |
| 53 | b.correct.06 | Wow, joli coup ! | B | 2.5 | [ ] |
| 54 | b.correct.07 | Excellent raisonnement ! | B | 2.5 | [ ] |
| 55 | b.correct.08 | Voilà, c’est exact. Chapeau ! | B | 3 | [ ] |
| 56 | b.correct.09 | Ça, c’est de la belle réflexion ! | B | 4 | [ ] |
| 57 | b.correct.10 | Yes ! C’est la bonne réponse. | B | 3.5 | [ ] |
| 58 | b.again.01 | Presque ! Essaie encore. | G | 2.5 | [ ] |
| 59 | b.again.02 | Pas tout à fait, mais tu es sur la bonne piste. | G | 5.5 | [ ] |
| 60 | b.again.03 | Hmm, regardons ça encore une fois. | G | 3.5 | [ ] |

## fr-CA session 3 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 61 | b.again.04 | Pas encore. On réessaie ensemble ? | G | 3.5 | [ ] |
| 62 | b.again.05 | Bonne tentative ! Retente ta chance. | G | 3.5 | [ ] |
| 63 | b.again.06 | Ce n’est pas celle-là, mais chercher, c’est apprendre. | G | 4.5 | [ ] |
| 64 | b.again.07 | Oups, petit détour ! Tu peux réessayer. | G | 4 | [ ] |
| 65 | b.again.08 | Respire, relis, et essaie encore. | G | 3 | [ ] |
| 66 | b.hint.01 | Un petit coup de pouce : relis la question lentement. | H | 5.5 | [ ] |
| 67 | b.hint.02 | Un indice : cherche les nombres dans l’histoire. | H | 4.5 | [ ] |
| 68 | b.hint.03 | Essaie de faire un dessin dans ta tête. | H | 4.5 | [ ] |
| 69 | b.hint.04 | Tu peux compter sur tes doigts, c’est permis ! | H | 5 | [ ] |
| 70 | b.hint.05 | Demande-toi : est-ce qu’on ajoute ou est-ce qu’on enlève ? | H | 5.5 | [ ] |
| 71 | b.hint.06 | Pars d’un petit nombre et avance pas à pas. | H | 5 | [ ] |
| 72 | b.hint.07 | Découpe le problème en petites parties. | H | 3.5 | [ ] |
| 73 | b.encourage.01 | Tu peux le faire, je suis là. | G | 4 | [ ] |
| 74 | b.encourage.02 | Chaque essai t’apprend quelque chose. | G | 3 | [ ] |
| 75 | b.encourage.03 | Pas de souci, on repart sur un autre sentier. | G | 5 | [ ] |
| 76 | b.encourage.04 | Tu es plus capable que tu ne le crois. | G | 5 | [ ] |
| 77 | b.encourage.05 | Ici, on a le droit de chercher. | G | 4 | [ ] |
| 78 | b.encourage.06 | Je crois en toi, vraiment. | G | 3 | [ ] |
| 79 | b.streak.01 | Ça fait plusieurs jours de suite, bravo ! | B | 4.5 | [ ] |
| 80 | b.streak.02 | Ta série continue, quelle belle habitude ! | B | 4 | [ ] |
| 81 | b.streak.03 | Un jour de plus dans ta série ! | B | 4.5 | [ ] |
| 82 | b.card.01 | Tu as gagné une nouvelle carte ! | B | 4 | [ ] |
| 83 | b.card.02 | Une carte pour ta collection, bravo ! | B | 4 | [ ] |
| 84 | b.card.03 | Regarde, une carte rien que pour toi ! | B | 4.5 | [ ] |
| 85 | b.chapterpass.01 | Bravo, chapitre réussi ! | B | 2.5 | [ ] |
| 86 | b.chapterpass.02 | Super travail, continue comme ça ! | B | 3.5 | [ ] |
| 87 | b.chapterpass.03 | Quelle belle réussite ! | B | 2.5 | [ ] |
| 88 | b.chapterpass.04 | Excellent, tu progresses bien ! | B | 3 | [ ] |
| 89 | b.chapterpass.05 | Génial, la carte est à toi ! | B | 4 | [ ] |
| 90 | b.chapterpass.06 | Chapeau, c’était du beau travail ! | B | 3.5 | [ ] |

## fr-CA session 4 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 91 | b.chaptermiss.01 | Presque ! On réessaie ensemble une autre fois. | G | 4.5 | [ ] |
| 92 | b.chaptermiss.02 | Pas encore, et c’est normal. À bientôt pour un autre essai ! | G | 6 | [ ] |
| 93 | b.chaptermiss.03 | On apprend en essayant. Reviens quand tu veux ! | G | 5 | [ ] |
| 94 | b.chaptermiss.04 | Courage ! Tu y es presque. | G | 3.5 | [ ] |
| 95 | b.detour.01 | Un petit chemin de côté. | S | 3 | [ ] |
| 96 | b.detour.02 | Tiens, une autre route. | S | 2.5 | [ ] |
| 97 | b.detour.03 | On change de sentier. | S | 2.5 | [ ] |
| 98 | b.detour.04 | Le chemin tourne. | S | 2.5 | [ ] |
| 99 | b.detour.05 | Et si on bifurquait ? | S | 3 | [ ] |
| 100 | b.surprise.01 | Oh, un coup de vent ! | Q | 3.5 | [ ] |
| 101 | b.surprise.02 | Voilà un rebondissement ! | Q | 2.5 | [ ] |
| 102 | b.surprise.03 | Quelque chose a bougé. | Q | 2.5 | [ ] |
| 103 | b.surprise.04 | Ça alors, du neuf ! | Q | 3 | [ ] |
| 104 | b.surprise.05 | L’histoire fait une pirouette. | Q | 2.5 | [ ] |
| 105 | d.world.nature | Monde Nature. Explore la forêt et ses secrets. | W | 4.5 | [ ] |
| 106 | d.world.espace | Monde Espace. Une mission vers les étoiles. | W | 4 | [ ] |
| 107 | d.world.cuisine | Monde Cuisine. Un grand repas à préparer. | W | 4 | [ ] |
| 108 | d.world.sport | Monde Sport. Le grand match approche. | W | 3.5 | [ ] |
| 109 | d.world.construction | Monde Construction. Bâtis quelque chose d’incroyable. | W | 3.5 | [ ] |
| 110 | d.world.medecine | Monde Médecine. Soigne et sauve la journée. | W | 4 | [ ] |
| 111 | d.world.science | Monde Science. Le laboratoire a besoin de toi. | W | 4.5 | [ ] |
| 112 | d.world.echecs | Monde Échecs. Un tournoi t’attend. | W | 3 | [ ] |
| 113 | d.world.ocean | Monde Océan. Plonge au cœur du récif. | W | 4 | [ ] |
| 114 | d.world.dinosaures | Monde Dinosaures. Des os géants t’attendent. | W | 3.5 | [ ] |
| 115 | d.world.animaux | Monde Animaux. C’est l’heure du repas à la ferme. | W | 5 | [ ] |
| 116 | d.world.musique | Monde Musique. Le concert commence bientôt. | W | 3.5 | [ ] |
| 117 | d.world.transport | Monde Transports. Tout le monde à bord. | W | 4 | [ ] |
| 118 | d.world.pirates | Monde Pirates. Une carte au trésor à suivre. | W | 4.5 | [ ] |
| 119 | d.world.marche | Monde Marché. Compte tes pièces au marché. | W | 4 | [ ] |
| 120 | d.world.meteo | Monde Météo. Quel temps fera-t-il demain ?. | W | 4 | [ ] |

## fr-CA session 5 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 121 | d.world.robots | Monde Robots. Programme ton robot. | W | 3 | [ ] |
| 122 | d.world.arts | Monde Arts. L’atelier est prêt. | W | 3 | [ ] |
| 123 | d.world.hiver | Monde Hiver. La cabane à sucre ouvre ses portes. | W | 5 | [ ] |
| 124 | d.world.jeux | Monde Jeux. C’est la soirée de jeux. | W | 4 | [ ] |
| 125 | d.chapter.generic.01 | Un nouveau chapitre commence. | W | 2.5 | [ ] |
| 126 | d.chapter.generic.02 | Voici le chapitre suivant. | W | 2.5 | [ ] |
| 127 | d.chapter.generic.03 | Prêts pour la suite de l’aventure ? | W | 4 | [ ] |
| 128 | d.chapter.generic.04 | On tourne la page. | W | 2.5 | [ ] |
| 129 | d.chapter.nature.feuilles | Les feuilles de l’automne. | W | 2.5 | [ ] |
| 130 | d.chapter.nature.sentier | Le sentier des nombres. | W | 2.5 | [ ] |
| 131 | d.chapter.nature.campement | Le campement. | W | 2 | [ ] |
| 132 | d.chapter.nature.observer | Les oiseaux du matin. | W | 2.5 | [ ] |
| 133 | d.chapter.nature.rivieres | Les rivières. | W | 2 | [ ] |
| 134 | d.chapter.espace.decompte | La fiche de lancement. | W | 2.5 | [ ] |
| 135 | d.chapter.espace.fusee | Le plein de la fusée. | W | 3 | [ ] |
| 136 | d.chapter.espace.orbites | Les panneaux solaires. | W | 2.5 | [ ] |
| 137 | d.chapter.espace.horloge | L’horloge de la base. | W | 2.5 | [ ] |
| 138 | d.chapter.espace.distances | Les distances. | W | 2 | [ ] |
| 139 | d.chapter.cuisine.fournee | La fournée de biscuits. | W | 2.5 | [ ] |
| 140 | d.chapter.cuisine.recette | La recette de pizza. | W | 2.5 | [ ] |
| 141 | d.chapter.cuisine.mesures | Les mesures de la recette. | W | 3 | [ ] |
| 142 | d.chapter.cuisine.partage | Le grand partage. | W | 2.5 | [ ] |
| 143 | d.chapter.cuisine.tablee | La grande tablée. | W | 2.5 | [ ] |
| 144 | d.chapter.sport.points | Les points du match. | W | 2.5 | [ ] |
| 145 | d.chapter.sport.classement | Le tableau des résultats. | W | 2.5 | [ ] |
| 146 | d.chapter.sport.relais | Le relais. | W | 2 | [ ] |
| 147 | d.chapter.sport.chrono | La séance d’entraînement. | W | 2.5 | [ ] |
| 148 | d.chapter.sport.tournoi | Le tirage au sort. | W | 2.5 | [ ] |
| 149 | d.chapter.construction.formes | Les plans de la cabane. | W | 3 | [ ] |
| 150 | d.chapter.construction.solides | Les blocs de construction. | W | 2.5 | [ ] |

## fr-CA session 6 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 151 | d.chapter.construction.perimetre | Le tour du plancher. | W | 2.5 | [ ] |
| 152 | d.chapter.construction.surface | Le mur à peindre. | W | 2.5 | [ ] |
| 153 | d.chapter.construction.regles | La règle et le mètre. | W | 3 | [ ] |
| 154 | d.chapter.medecine.doses | Les doses de la clinique. | W | 3 | [ ] |
| 155 | d.chapter.medecine.poids | Peser les patients. | W | 2.5 | [ ] |
| 156 | d.chapter.medecine.horaire | L’horaire des visites. | W | 2.5 | [ ] |
| 157 | d.chapter.medecine.dossier | Le dossier des visites. | W | 2.5 | [ ] |
| 158 | d.chapter.medecine.pansements | Les boîtes de pansements. | W | 2.5 | [ ] |
| 159 | d.chapter.science.mesurer | Mesurer au laboratoire. | W | 2.5 | [ ] |
| 160 | d.chapter.science.hasard | Le jeu du hasard. | W | 2.5 | [ ] |
| 161 | d.chapter.science.resultats | Les résultats de l’expérience. | W | 2.5 | [ ] |
| 162 | d.chapter.science.suites | Les suites de mesures. | W | 2.5 | [ ] |
| 163 | d.chapter.science.melanges | Les mélanges. | W | 2 | [ ] |
| 164 | d.chapter.echecs.cases | Les cases de l’échiquier. | W | 2.5 | [ ] |
| 165 | d.chapter.echecs.parcours | Le parcours du cavalier. | W | 2.5 | [ ] |
| 166 | d.chapter.echecs.valeurs | La valeur des pièces. | W | 2.5 | [ ] |
| 167 | d.chapter.echecs.motifs | Les motifs du damier. | W | 2.5 | [ ] |
| 168 | d.chapter.echecs.partie | La partie. | W | 2 | [ ] |
| 169 | d.chapter.ocean.recif | Les poissons du récif. | W | 2.5 | [ ] |
| 170 | d.chapter.ocean.profondeur | La plongée. | W | 2 | [ ] |
| 171 | d.chapter.ocean.baleines | Les baleines du Saint-Laurent. | W | 2.5 | [ ] |
| 172 | d.chapter.ocean.sous-marin | Le sous-marin. | W | 2 | [ ] |
| 173 | d.chapter.dinosaures.fossiles | Les fossiles. | W | 2 | [ ] |
| 174 | d.chapter.dinosaures.os | Les os du squelette. | W | 2.5 | [ ] |
| 175 | d.chapter.dinosaures.geants | Les géants. | W | 2 | [ ] |
| 176 | d.chapter.dinosaures.oeufs | Les nids d’œufs. | W | 2.5 | [ ] |
| 177 | d.chapter.animaux.ferme | La ferme. | W | 2 | [ ] |
| 178 | d.chapter.animaux.pattes | Les pattes. | W | 2 | [ ] |
| 179 | d.chapter.animaux.zoo | Le zoo. | W | 2 | [ ] |
| 180 | d.chapter.animaux.pairs | Pair ou impair. | W | 2.5 | [ ] |

## fr-CA session 7 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 181 | d.chapter.musique.rythme | Le rythme. | W | 2 | [ ] |
| 182 | d.chapter.musique.mesures | Les mesures de la partition. | W | 3 | [ ] |
| 183 | d.chapter.musique.concert | Le concert. | W | 2 | [ ] |
| 184 | d.chapter.musique.instruments | Les instruments. | W | 2 | [ ] |
| 185 | d.chapter.transport.gare | La gare. | W | 2 | [ ] |
| 186 | d.chapter.transport.autobus | L’autobus scolaire. | W | 2 | [ ] |
| 187 | d.chapter.transport.pistes | Les pistes. | W | 2 | [ ] |
| 188 | d.chapter.transport.voyage | Le grand voyage. | W | 2.5 | [ ] |
| 189 | d.chapter.pirates.carte | La carte au trésor. | W | 2.5 | [ ] |
| 190 | d.chapter.pirates.tresor | Le coffre au trésor. | W | 2.5 | [ ] |
| 191 | d.chapter.pirates.butin | Le partage du butin. | W | 2.5 | [ ] |
| 192 | d.chapter.pirates.navire | Le navire. | W | 2 | [ ] |
| 193 | d.chapter.marche.pieces | Les pièces de monnaie. | W | 2.5 | [ ] |
| 194 | d.chapter.marche.prix | Les prix. | W | 2 | [ ] |
| 195 | d.chapter.marche.monnaie | La monnaie. | W | 2 | [ ] |
| 196 | d.chapter.marche.panier | Le panier. | W | 2 | [ ] |
| 197 | d.chapter.marche.economies | Les économies. | W | 2 | [ ] |
| 198 | d.chapter.meteo.pluie | La pluie. | W | 2 | [ ] |
| 199 | d.chapter.meteo.temperature | La température. | W | 2 | [ ] |
| 200 | d.chapter.meteo.prevision | La prévision. | W | 2 | [ ] |
| 201 | d.chapter.meteo.saisons | Les saisons. | W | 2 | [ ] |
| 202 | d.chapter.robots.programme | Le programme. | W | 2 | [ ] |
| 203 | d.chapter.robots.grille | La grille. | W | 2 | [ ] |
| 204 | d.chapter.robots.miroir | Le robot miroir. | W | 2.5 | [ ] |
| 205 | d.chapter.robots.boucles | Les boucles. | W | 2 | [ ] |
| 206 | d.chapter.arts.formes | Les formes. | W | 2 | [ ] |
| 207 | d.chapter.arts.frises | Les frises. | W | 2 | [ ] |
| 208 | d.chapter.arts.mosaique | La mosaïque. | W | 2 | [ ] |
| 209 | d.chapter.arts.symetrie | Les droites de l’atelier. | W | 2.5 | [ ] |
| 210 | d.chapter.hiver.erabliere | L’érablière. | W | 1.5 | [ ] |

## fr-CA session 8 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 211 | d.chapter.hiver.patinoire | La patinoire. | W | 2 | [ ] |
| 212 | d.chapter.hiver.neige | La neige. | W | 2 | [ ] |
| 213 | d.chapter.hiver.horaire | L’horaire du village d’hiver. | W | 2.5 | [ ] |
| 214 | d.chapter.jeux.des | Les dés. | W | 2 | [ ] |
| 215 | d.chapter.jeux.cartes | Les cartes. | W | 2 | [ ] |
| 216 | d.chapter.jeux.points | Les points. | W | 2 | [ ] |
| 217 | d.chapter.jeux.chance | La chance. | W | 2 | [ ] |
| 218 | f.gen.open.01 | Une nouvelle aventure commence, et tu en fais partie. | S | 5 | [ ] |
| 219 | f.gen.open.02 | Voici un nouveau chemin à explorer. | S | 3.5 | [ ] |
| 220 | f.gen.open.03 | Quelqu’un a besoin de ton aide ! | S | 4 | [ ] |
| 221 | f.gen.ok.01 | Bien joué ! L’histoire peut continuer. | S | 3.5 | [ ] |
| 222 | f.gen.ok.02 | Le compte est juste, et tout le monde est content. | S | 5.5 | [ ] |
| 223 | f.gen.ok.03 | Bravo ! Tu as trouvé le bon chemin. | S | 4.5 | [ ] |
| 224 | f.gen.dt.01 | Ce n’est pas tout à fait ça, mais tu as bien essayé. Prends ton temps. | S | 7.5 | [ ] |
| 225 | f.gen.dt.02 | Un petit détour, rien de grave. On réessaie quand tu veux. | S | 5.5 | [ ] |
| 226 | f.gen.dt.03 | Pas cette fois, et c’est normal. On apprend en essayant. | S | 5.5 | [ ] |
| 227 | f.gen.su.01 | Oh, l’histoire prend un tournant surprise ! | S | 4 | [ ] |
| 228 | f.gen.su.02 | Tiens, personne ne s’y attendait ! | S | 3.5 | [ ] |
| 229 | f.gen.su.03 | Quelle drôle de surprise ! | S | 3 | [ ] |
| 230 | f.nature.open | Dans la forêt, quelque chose bouge entre les feuilles. | S | 5 | [ ] |
| 231 | f.nature.ok | Le compte est juste, et la forêt s’en réjouit. | S | 5 | [ ] |
| 232 | f.nature.dt | Le compte n’est pas tout à fait juste. La forêt attend un autre essai. | S | 7 | [ ] |
| 233 | f.nature.su | Un coup de vent, et les feuilles se mettent à danser ! | S | 6 | [ ] |
| 234 | f.espace.open | Là-haut, les étoiles clignent. La mission peut commencer. | S | 4.5 | [ ] |
| 235 | f.espace.ok | Bien calculé ! La fusée suit la bonne route. | S | 5 | [ ] |
| 236 | f.espace.dt | La fusée dévie un peu. On peut corriger le cap. | S | 5.5 | [ ] |
| 237 | f.espace.su | Une comète passe tout près, quel spectacle ! | S | 4.5 | [ ] |
| 238 | f.cuisine.open | Ça sent bon dans la cuisine, un grand repas se prépare. | S | 5.5 | [ ] |
| 239 | f.cuisine.ok | La recette réussit, et ça sent encore meilleur. | S | 4.5 | [ ] |
| 240 | f.cuisine.dt | Les quantités ne sont pas tout à fait justes. On goûte et on ajuste. | S | 7 | [ ] |

## fr-CA session 9 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 241 | f.cuisine.su | Pouf ! Un nuage de farine s’envole dans la pièce. | S | 5.5 | [ ] |
| 242 | f.sport.open | Le stade est plein, le grand match approche. | S | 4.5 | [ ] |
| 243 | f.sport.ok | Quel beau geste ! Le ballon file droit au but. | S | 5.5 | [ ] |
| 244 | f.sport.dt | Le ballon frôle le poteau. On se prépare pour un autre tir. | S | 6 | [ ] |
| 245 | f.sport.su | Un rebond inattendu, et le public retient son souffle ! | S | 5.5 | [ ] |
| 246 | f.construction.open | Le chantier s’éveille, il y a un pont à bâtir. | S | 5.5 | [ ] |
| 247 | f.construction.ok | Bien calculé ! La structure est solide. | S | 4 | [ ] |
| 248 | f.construction.dt | Une poutre est un peu trop courte. On la mesure encore et on recommence. | S | 7 | [ ] |
| 249 | f.construction.su | Une grue tourne et dépose une surprise sur le chantier ! | S | 5.5 | [ ] |
| 250 | f.medecine.open | À la clinique, quelqu’un a besoin de ton aide. | S | 5 | [ ] |
| 251 | f.medecine.ok | Le bon calcul aide : tout le monde se sent mieux. | S | 5.5 | [ ] |
| 252 | f.medecine.dt | La dose n’est pas tout à fait juste. On vérifie ensemble, doucement. | S | 6 | [ ] |
| 253 | f.medecine.su | Un petit rire éclate dans la salle d’attente ! | S | 5 | [ ] |
| 254 | f.science.open | Dans le labo, les éprouvettes brillent et l’expérience commence. | S | 5 | [ ] |
| 255 | f.science.ok | Le mélange est juste, et la fiole fait des bulles ! | S | 5.5 | [ ] |
| 256 | f.science.dt | La réaction s’endort un peu. On refait l’expérience. | S | 4.5 | [ ] |
| 257 | f.science.su | Pschiit ! Un nuage de fumée colorée sort du flacon. | S | 5.5 | [ ] |
| 258 | f.echecs.open | Le plateau est prêt, les pièces attendent ton plan. | S | 5 | [ ] |
| 259 | f.echecs.ok | Beau coup ! Ton plan avance bien. | S | 4 | [ ] |
| 260 | f.echecs.dt | Le cavalier est bloqué. Il y a toujours une autre route. | S | 5.5 | [ ] |
| 261 | f.echecs.su | Un pion avance d’un pas que personne n’attendait ! | S | 5 | [ ] |
| 262 | f.ocean.open | Sous les vagues, un monde de couleurs t’attend. | S | 4.5 | [ ] |
| 263 | f.ocean.ok | Bien compté ! Les poissons dansent autour de toi. | S | 5 | [ ] |
| 264 | f.ocean.dt | Le courant nous pousse un peu à côté. On nage de nouveau. | S | 6 | [ ] |
| 265 | f.ocean.su | Une bulle géante passe et fait rire les poissons ! | S | 5.5 | [ ] |
| 266 | f.dinosaures.open | La vallée des dinosaures s’éveille doucement. | S | 3.5 | [ ] |
| 267 | f.dinosaures.ok | Le compte est bon, et un petit dinosaure fait un grand bond ! | S | 6.5 | [ ] |
| 268 | f.dinosaures.dt | Les traces ne sont pas tout à fait comptées. On regarde de plus près. | S | 7 | [ ] |
| 269 | f.dinosaures.su | Boum, boum ! De grands pas font vibrer le sol. | S | 5.5 | [ ] |
| 270 | f.animaux.open | À la ferme, les animaux t’attendent avec impatience. | S | 4.5 | [ ] |

## fr-CA session 10 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 271 | f.animaux.ok | Bien compté ! Tout le monde est content. | S | 4.5 | [ ] |
| 272 | f.animaux.dt | Il manque une petite bête dans le compte. On recompte. | S | 5.5 | [ ] |
| 273 | f.animaux.su | Cot cot ! Une poule se met à chanter très fort. | S | 5.5 | [ ] |
| 274 | f.musique.open | Les instruments s’accordent, le concert va commencer. | S | 4 | [ ] |
| 275 | f.musique.ok | Quel beau rythme ! La mélodie est parfaite. | S | 4.5 | [ ] |
| 276 | f.musique.dt | Une note sonne un peu faux. On reprend la mesure. | S | 5.5 | [ ] |
| 277 | f.musique.su | Pom pom pom ! Le tambour joue un rythme surprise. | S | 5.5 | [ ] |
| 278 | f.transport.open | Les moteurs ronronnent, un grand voyage commence. | S | 4 | [ ] |
| 279 | f.transport.ok | Bien calculé ! Le trajet se passe à merveille. | S | 5 | [ ] |
| 280 | f.transport.dt | On a pris un petit détour. Pas de panique, on repart. | S | 5.5 | [ ] |
| 281 | f.transport.su | Pouet pouet ! Un train siffle au loin. | S | 4.5 | [ ] |
| 282 | f.pirates.open | Le bateau tangue, une carte au trésor t’attend. | S | 4.5 | [ ] |
| 283 | f.pirates.ok | Bien compté ! Le coffre s’ouvre en grinçant. | S | 4.5 | [ ] |
| 284 | f.pirates.dt | Le compte n’est pas juste. La carte nous attend pour un autre essai. | S | 6.5 | [ ] |
| 285 | f.pirates.su | Un perroquet crie un secret depuis le mât ! | S | 5 | [ ] |
| 286 | f.marche.open | Au marché, les étals sont pleins de couleurs et de bonnes odeurs. | S | 6 | [ ] |
| 287 | f.marche.ok | Le compte est bon, et la marchande sourit. | S | 4.5 | [ ] |
| 288 | f.marche.dt | La monnaie n’est pas tout à fait juste. On recompte les pièces. | S | 6 | [ ] |
| 289 | f.marche.su | Une pomme roule de l’étal, vite, on la rattrape ! | S | 5.5 | [ ] |
| 290 | f.meteo.open | Le ciel change, les nuages se rassemblent. | S | 4 | [ ] |
| 291 | f.meteo.ok | Bien lu ! La prévision est juste. | S | 4 | [ ] |
| 292 | f.meteo.dt | La prévision n’est pas tout à fait juste. Regardons le thermomètre encore une fois. | S | 7 | [ ] |
| 293 | f.meteo.su | Un arc-en-ciel apparaît juste après la pluie ! | S | 4.5 | [ ] |
| 294 | f.robots.open | Dans l’atelier, les robots bourdonnent et clignotent. | S | 4 | [ ] |
| 295 | f.robots.ok | Calcul réussi ! Le robot fait une petite danse. | S | 5 | [ ] |
| 296 | f.robots.dt | Le robot a un petit bug. On le règle ensemble. | S | 5.5 | [ ] |
| 297 | f.robots.su | Bip boup ! Le robot sort un tour de magie. | S | 5.5 | [ ] |
| 298 | f.arts.open | Dans l’atelier, les pinceaux sont prêts et la toile est vide. | S | 5.5 | [ ] |
| 299 | f.arts.ok | Bien mesuré ! Les couleurs s’accordent à merveille. | S | 4.5 | [ ] |
| 300 | f.arts.dt | Les couleurs ne sont pas tout à fait dosées. On mélange encore. | S | 6 | [ ] |

## fr-CA session 11 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 301 | f.arts.su | Plouf ! Une goutte de peinture tombe et fait une étoile. | S | 5.5 | [ ] |
| 302 | f.hiver.open | La neige tombe doucement, tout est blanc et calme. | S | 5 | [ ] |
| 303 | f.hiver.ok | Bien compté ! Le bonhomme de neige sourit. | S | 4.5 | [ ] |
| 304 | f.hiver.dt | Il manque quelques boules de neige. On en roule d’autres. | S | 5.5 | [ ] |
| 305 | f.hiver.su | Frouff ! La neige se met à tourbillonner autour de toi. | S | 5.5 | [ ] |
| 306 | f.jeux.open | La soirée de jeux commence, les dés sont prêts. | S | 5 | [ ] |
| 307 | f.jeux.ok | Beau coup ! Les points s’additionnent bien. | S | 4 | [ ] |
| 308 | f.jeux.dt | Le total n’est pas tout à fait juste. On relance la partie. | S | 6 | [ ] |
| 309 | f.jeux.su | Les dés roulent, et voilà un double ! | S | 4.5 | [ ] |
| 310 | c.atom.fr.01 | zéro | N | 1.5 | [ ] |
| 311 | c.atom.fr.02 | un | N | 1.5 | [ ] |
| 312 | c.atom.fr.03 | deux | N | 1.5 | [ ] |
| 313 | c.atom.fr.04 | trois | N | 1.5 | [ ] |
| 314 | c.atom.fr.05 | quatre | N | 1.5 | [ ] |
| 315 | c.atom.fr.06 | cinq | N | 1.5 | [ ] |
| 316 | c.atom.fr.07 | six | N | 1.5 | [ ] |
| 317 | c.atom.fr.08 | sept | N | 1.5 | [ ] |
| 318 | c.atom.fr.09 | huit | N | 1.5 | [ ] |
| 319 | c.atom.fr.10 | neuf | N | 1.5 | [ ] |
| 320 | c.atom.fr.11 | dix | N | 1.5 | [ ] |
| 321 | c.atom.fr.12 | onze | N | 1.5 | [ ] |
| 322 | c.atom.fr.13 | douze | N | 1.5 | [ ] |
| 323 | c.atom.fr.14 | treize | N | 1.5 | [ ] |
| 324 | c.atom.fr.15 | quatorze | N | 1.5 | [ ] |
| 325 | c.atom.fr.16 | quinze | N | 1.5 | [ ] |
| 326 | c.atom.fr.17 | seize | N | 1.5 | [ ] |
| 327 | c.atom.fr.18 | vingt | N | 1.5 | [ ] |
| 328 | c.atom.fr.19 | trente | N | 1.5 | [ ] |
| 329 | c.atom.fr.20 | quarante | N | 1.5 | [ ] |
| 330 | c.atom.fr.21 | cinquante | N | 1.5 | [ ] |

## fr-CA session 12 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 331 | c.atom.fr.22 | soixante | N | 1.5 | [ ] |
| 332 | c.atom.fr.23 | quatre-vingt | N | 1.5 | [ ] |
| 333 | c.atom.fr.24 | quatre-vingts | N | 1.5 | [ ] |
| 334 | c.atom.fr.25 | cent | N | 1.5 | [ ] |
| 335 | c.atom.fr.26 | cents | N | 1.5 | [ ] |
| 336 | c.atom.fr.27 | mille | N | 1.5 | [ ] |
| 337 | c.atom.fr.28 | million | N | 1.5 | [ ] |
| 338 | c.atom.fr.29 | millions | N | 1.5 | [ ] |
| 339 | c.atom.fr.30 | et | N | 1.5 | [ ] |
| 340 | c.atom.fr.31 | virgule | N | 1.5 | [ ] |
| 341 | c.num.000 | zéro (x2) | N | 1.5 | [ ] |
| 342 | c.num.001 | un (x2) | N | 1.5 | [ ] |
| 343 | c.num.002 | deux (x2) | N | 1.5 | [ ] |
| 344 | c.num.003 | trois (x2) | N | 1.5 | [ ] |
| 345 | c.num.004 | quatre (x2) | N | 1.5 | [ ] |
| 346 | c.num.005 | cinq (x2) | N | 1.5 | [ ] |
| 347 | c.num.006 | six (x2) | N | 1.5 | [ ] |
| 348 | c.num.007 | sept (x2) | N | 1.5 | [ ] |
| 349 | c.num.008 | huit (x2) | N | 1.5 | [ ] |
| 350 | c.num.009 | neuf (x2) | N | 1.5 | [ ] |
| 351 | c.num.010 | dix (x2) | N | 1.5 | [ ] |
| 352 | c.num.011 | onze (x2) | N | 1.5 | [ ] |
| 353 | c.num.012 | douze (x2) | N | 1.5 | [ ] |
| 354 | c.num.013 | treize (x2) | N | 1.5 | [ ] |
| 355 | c.num.014 | quatorze (x2) | N | 1.5 | [ ] |
| 356 | c.num.015 | quinze (x2) | N | 1.5 | [ ] |
| 357 | c.num.016 | seize (x2) | N | 1.5 | [ ] |
| 358 | c.num.017 | dix-sept (x2) | N | 1.5 | [ ] |
| 359 | c.num.018 | dix-huit (x2) | N | 1.5 | [ ] |
| 360 | c.num.019 | dix-neuf (x2) | N | 1.5 | [ ] |

## fr-CA session 13 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 361 | c.num.020 | vingt (x2) | N | 1.5 | [ ] |
| 362 | c.num.021 | vingt et un (x2) | N | 2.5 | [ ] |
| 363 | c.num.022 | vingt-deux (x2) | N | 1.5 | [ ] |
| 364 | c.num.023 | vingt-trois (x2) | N | 1.5 | [ ] |
| 365 | c.num.024 | vingt-quatre (x2) | N | 1.5 | [ ] |
| 366 | c.num.025 | vingt-cinq (x2) | N | 1.5 | [ ] |
| 367 | c.num.026 | vingt-six (x2) | N | 1.5 | [ ] |
| 368 | c.num.027 | vingt-sept (x2) | N | 1.5 | [ ] |
| 369 | c.num.028 | vingt-huit (x2) | N | 1.5 | [ ] |
| 370 | c.num.029 | vingt-neuf (x2) | N | 1.5 | [ ] |
| 371 | c.num.030 | trente (x2) | N | 1.5 | [ ] |
| 372 | c.num.031 | trente et un (x2) | N | 2.5 | [ ] |
| 373 | c.num.032 | trente-deux (x2) | N | 1.5 | [ ] |
| 374 | c.num.033 | trente-trois (x2) | N | 1.5 | [ ] |
| 375 | c.num.034 | trente-quatre (x2) | N | 1.5 | [ ] |
| 376 | c.num.035 | trente-cinq (x2) | N | 1.5 | [ ] |
| 377 | c.num.036 | trente-six (x2) | N | 1.5 | [ ] |
| 378 | c.num.037 | trente-sept (x2) | N | 1.5 | [ ] |
| 379 | c.num.038 | trente-huit (x2) | N | 1.5 | [ ] |
| 380 | c.num.039 | trente-neuf (x2) | N | 1.5 | [ ] |
| 381 | c.num.040 | quarante (x2) | N | 1.5 | [ ] |
| 382 | c.num.041 | quarante et un (x2) | N | 2.5 | [ ] |
| 383 | c.num.042 | quarante-deux (x2) | N | 1.5 | [ ] |
| 384 | c.num.043 | quarante-trois (x2) | N | 1.5 | [ ] |
| 385 | c.num.044 | quarante-quatre (x2) | N | 1.5 | [ ] |
| 386 | c.num.045 | quarante-cinq (x2) | N | 1.5 | [ ] |
| 387 | c.num.046 | quarante-six (x2) | N | 1.5 | [ ] |
| 388 | c.num.047 | quarante-sept (x2) | N | 1.5 | [ ] |
| 389 | c.num.048 | quarante-huit (x2) | N | 1.5 | [ ] |
| 390 | c.num.049 | quarante-neuf (x2) | N | 1.5 | [ ] |

## fr-CA session 14 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 391 | c.num.050 | cinquante (x2) | N | 1.5 | [ ] |
| 392 | c.num.051 | cinquante et un (x2) | N | 2.5 | [ ] |
| 393 | c.num.052 | cinquante-deux (x2) | N | 1.5 | [ ] |
| 394 | c.num.053 | cinquante-trois (x2) | N | 1.5 | [ ] |
| 395 | c.num.054 | cinquante-quatre (x2) | N | 1.5 | [ ] |
| 396 | c.num.055 | cinquante-cinq (x2) | N | 1.5 | [ ] |
| 397 | c.num.056 | cinquante-six (x2) | N | 1.5 | [ ] |
| 398 | c.num.057 | cinquante-sept (x2) | N | 1.5 | [ ] |
| 399 | c.num.058 | cinquante-huit (x2) | N | 1.5 | [ ] |
| 400 | c.num.059 | cinquante-neuf (x2) | N | 1.5 | [ ] |
| 401 | c.num.060 | soixante (x2) | N | 1.5 | [ ] |
| 402 | c.num.061 | soixante et un (x2) | N | 2.5 | [ ] |
| 403 | c.num.062 | soixante-deux (x2) | N | 1.5 | [ ] |
| 404 | c.num.063 | soixante-trois (x2) | N | 1.5 | [ ] |
| 405 | c.num.064 | soixante-quatre (x2) | N | 1.5 | [ ] |
| 406 | c.num.065 | soixante-cinq (x2) | N | 1.5 | [ ] |
| 407 | c.num.066 | soixante-six (x2) | N | 1.5 | [ ] |
| 408 | c.num.067 | soixante-sept (x2) | N | 1.5 | [ ] |
| 409 | c.num.068 | soixante-huit (x2) | N | 1.5 | [ ] |
| 410 | c.num.069 | soixante-neuf (x2) | N | 1.5 | [ ] |
| 411 | c.num.070 | soixante-dix (x2) | N | 1.5 | [ ] |
| 412 | c.num.071 | soixante et onze (x2) | N | 2.5 | [ ] |
| 413 | c.num.072 | soixante-douze (x2) | N | 1.5 | [ ] |
| 414 | c.num.073 | soixante-treize (x2) | N | 1.5 | [ ] |
| 415 | c.num.074 | soixante-quatorze (x2) | N | 1.5 | [ ] |
| 416 | c.num.075 | soixante-quinze (x2) | N | 1.5 | [ ] |
| 417 | c.num.076 | soixante-seize (x2) | N | 1.5 | [ ] |
| 418 | c.num.077 | soixante-dix-sept (x2) | N | 1.5 | [ ] |
| 419 | c.num.078 | soixante-dix-huit (x2) | N | 1.5 | [ ] |
| 420 | c.num.079 | soixante-dix-neuf (x2) | N | 1.5 | [ ] |

## fr-CA session 15 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 421 | c.num.080 | quatre-vingts (x2) | N | 1.5 | [ ] |
| 422 | c.num.081 | quatre-vingt-un (x2) | N | 1.5 | [ ] |
| 423 | c.num.082 | quatre-vingt-deux (x2) | N | 1.5 | [ ] |
| 424 | c.num.083 | quatre-vingt-trois (x2) | N | 1.5 | [ ] |
| 425 | c.num.084 | quatre-vingt-quatre (x2) | N | 1.5 | [ ] |
| 426 | c.num.085 | quatre-vingt-cinq (x2) | N | 1.5 | [ ] |
| 427 | c.num.086 | quatre-vingt-six (x2) | N | 1.5 | [ ] |
| 428 | c.num.087 | quatre-vingt-sept (x2) | N | 1.5 | [ ] |
| 429 | c.num.088 | quatre-vingt-huit (x2) | N | 1.5 | [ ] |
| 430 | c.num.089 | quatre-vingt-neuf (x2) | N | 1.5 | [ ] |
| 431 | c.num.090 | quatre-vingt-dix (x2) | N | 1.5 | [ ] |
| 432 | c.num.091 | quatre-vingt-onze (x2) | N | 1.5 | [ ] |
| 433 | c.num.092 | quatre-vingt-douze (x2) | N | 1.5 | [ ] |
| 434 | c.num.093 | quatre-vingt-treize (x2) | N | 1.5 | [ ] |
| 435 | c.num.094 | quatre-vingt-quatorze (x2) | N | 1.5 | [ ] |
| 436 | c.num.095 | quatre-vingt-quinze (x2) | N | 1.5 | [ ] |
| 437 | c.num.096 | quatre-vingt-seize (x2) | N | 1.5 | [ ] |
| 438 | c.num.097 | quatre-vingt-dix-sept (x2) | N | 1.5 | [ ] |
| 439 | c.num.098 | quatre-vingt-dix-huit (x2) | N | 1.5 | [ ] |
| 440 | c.num.099 | quatre-vingt-dix-neuf (x2) | N | 1.5 | [ ] |
| 441 | c.num.100 | cent (x2) | N | 1.5 | [ ] |
| 442 | c.unit.cm | centimètre | N | 1.5 | [ ] |
| 443 | c.unit.m | mètre | N | 1.5 | [ ] |
| 444 | c.unit.km | kilomètre | N | 1.5 | [ ] |
| 445 | c.unit.g | gramme | N | 1.5 | [ ] |
| 446 | c.unit.kg | kilogramme | N | 1.5 | [ ] |
| 447 | c.unit.l | litre | N | 1.5 | [ ] |
| 448 | c.unit.ml | millilitre | N | 1.5 | [ ] |
| 449 | c.unit.s | seconde | N | 1.5 | [ ] |
| 450 | c.unit.min | minute | N | 1.5 | [ ] |

## fr-CA session 16 (29 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 451 | c.unit.h | heure | N | 1.5 | [ ] |
| 452 | c.unit.day | jour | N | 1.5 | [ ] |
| 453 | c.unit.week | semaine | N | 1.5 | [ ] |
| 454 | c.unit.month | mois | N | 1.5 | [ ] |
| 455 | c.unit.year | an | N | 1.5 | [ ] |
| 456 | c.unit.dollar | dollar | N | 1.5 | [ ] |
| 457 | c.unit.cent | cent | N | 1.5 | [ ] |
| 458 | c.unit.deg | degré | N | 1.5 | [ ] |
| 459 | c.unit.pct | pour cent | N | 2 | [ ] |
| 460 | c.unit.sq | carré | N | 1.5 | [ ] |
| 461 | c.unit.unit | unité | N | 1.5 | [ ] |
| 462 | c.op.plus | plus | N | 1.5 | [ ] |
| 463 | c.op.minus | moins | N | 1.5 | [ ] |
| 464 | c.op.times | fois | N | 1.5 | [ ] |
| 465 | c.op.div | divisé par | N | 2 | [ ] |
| 466 | c.op.eq | égale | N | 1.5 | [ ] |
| 467 | c.op.howmany | combien | N | 1.5 | [ ] |
| 468 | c.op.half | demi | N | 1.5 | [ ] |
| 469 | c.op.third | tiers | N | 1.5 | [ ] |
| 470 | c.op.quarter | quart | N | 1.5 | [ ] |
| 471 | c.op.over | sur | N | 1.5 | [ ] |
| 472 | c.op.total | en tout | N | 2 | [ ] |
| 473 | c.op.more | de plus | N | 2 | [ ] |
| 474 | c.op.less | de moins | N | 2 | [ ] |
| 475 | c.op.left | il en reste | N | 2.5 | [ ] |
| 476 | c.op.each | chacun | N | 1.5 | [ ] |
| 477 | c.op.gt | plus grand que | N | 2.5 | [ ] |
| 478 | c.op.lt | plus petit que | N | 2.5 | [ ] |
| 479 | c.op.eqto | égal à | N | 2 | [ ] |

---

# English (en-CA)

Numa is a warm, curious, kind owl with a little playfulness. Natural Canadian English, smile audible in the voice, relaxed pace, as if talking to a child of 6 to 9 you like. Never disappointed, never a sigh, never pressure: a wrong answer is a detour, not a failure. Keep the same person across sessions: same mic, same distance, same energy.

## en-CA session 1 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 1 | a.welcome.01 | Hi there, so glad you are back! | W | 4 | [ ] |
| 2 | a.welcome.02 | Ready for a new story? | W | 3 | [ ] |
| 3 | a.welcome.03 | There you are! I saved your spot. | W | 4 | [ ] |
| 4 | a.welcome.04 | Hello! Adventure was waiting for you. | W | 3.5 | [ ] |
| 5 | a.welcome.05 | Settle in, we have lots to discover. | W | 4 | [ ] |
| 6 | a.welcome.06 | I am so happy you are here. | W | 4 | [ ] |
| 7 | a.think.01 | Hmm, let us look at this together. | H | 4 | [ ] |
| 8 | a.think.02 | Take your time, I am thinking with you. | H | 4.5 | [ ] |
| 9 | a.think.03 | Good one! Shall we turn it around? | H | 4 | [ ] |
| 10 | a.think.04 | What if we made small groups? | H | 3.5 | [ ] |
| 11 | a.think.05 | No rush at all here. | H | 3 | [ ] |
| 12 | a.think.06 | Picture it in your head, it sometimes helps. | H | 4.5 | [ ] |
| 13 | a.curious.01 | Oh, what is hiding over there? | Q | 3.5 | [ ] |
| 14 | a.curious.02 | Smells like adventure, does it not? | Q | 3.5 | [ ] |
| 15 | a.curious.03 | I wonder what you will choose. | Q | 3.5 | [ ] |
| 16 | a.curious.04 | My feathers are tingling, there is a secret! | Q | 4.5 | [ ] |
| 17 | a.curious.05 | What if we looked a little closer? | Q | 4 | [ ] |
| 18 | a.amazed.01 | Oh my, what a twist! | Q | 3 | [ ] |
| 19 | a.amazed.02 | Did you see that? My beak is hanging open! | Q | 5 | [ ] |
| 20 | a.amazed.03 | Amazing, the story just bounced! | Q | 3 | [ ] |
| 21 | a.amazed.04 | Wow, I really did not expect that. | Q | 4 | [ ] |
| 22 | a.amazed.05 | Even I did not see that coming! | Q | 4 | [ ] |
| 23 | a.exit.01 | Your story will wait. Come back whenever you like. | W | 5 | [ ] |
| 24 | a.exit.02 | See you soon! I will keep your spot warm. | W | 5 | [ ] |
| 25 | a.exit.03 | Have a good rest, the next part will be here tomorrow. | W | 5.5 | [ ] |
| 26 | a.exit.04 | What a lovely adventure today, see you soon! | W | 4.5 | [ ] |
| 27 | a.exit.05 | Thanks for playing with me. | W | 3 | [ ] |
| 28 | a.exit.06 | Sweet dreams of numbers and stars. | W | 3.5 | [ ] |
| 29 | a.ui.sample | Hello! I am Numa. Shall we go? | W | 4 | [ ] |
| 30 | a.ui.name | I am Numa the owl. | W | 3 | [ ] |

## en-CA session 2 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 31 | a.ui.play | Tap here to play. | W | 2.5 | [ ] |
| 32 | a.ui.pickworld | Pick your adventure. | W | 2.5 | [ ] |
| 33 | a.ui.gift | Tap the gift to open it. | W | 3.5 | [ ] |
| 34 | a.ui.loading | The story is on its way. | W | 3.5 | [ ] |
| 35 | a.ui.slow | The story is taking a moment. We will wait together. | W | 5.5 | [ ] |
| 36 | a.ui.fallback | This part of the story is told in a short version. | W | 5.5 | [ ] |
| 37 | a.ui.askhint | Want a little hint? Tap the button. | W | 4 | [ ] |
| 38 | a.ui.listen | Tap to hear me again. | W | 3 | [ ] |
| 39 | a.ui.scroll | Scroll to discover more. | W | 2.5 | [ ] |
| 40 | a.ui.cards | Here is your card collection. | W | 3 | [ ] |
| 41 | a.ui.progress | Here are your streak and your progress. | W | 4 | [ ] |
| 42 | a.ui.back | Shall we go back? | W | 2.5 | [ ] |
| 43 | a.ui.next | Shall we keep going? | W | 2.5 | [ ] |
| 44 | a.ui.choose | Choose an answer. | W | 2.5 | [ ] |
| 45 | a.ui.type | Type your answer with the keys. | W | 3.5 | [ ] |
| 46 | a.ui.noclock | No clock here. Take all the time you like. | W | 5 | [ ] |
| 47 | a.ui.sleepy | A little break? I will be here when you come back. | W | 5.5 | [ ] |
| 48 | b.correct.01 | Nice one, well spotted! | B | 2.5 | [ ] |
| 49 | b.correct.02 | Yes, that is it! | B | 2.5 | [ ] |
| 50 | b.correct.03 | Exactly right! | B | 2 | [ ] |
| 51 | b.correct.04 | Great, you found it! | B | 2.5 | [ ] |
| 52 | b.correct.05 | Well played! | B | 2 | [ ] |
| 53 | b.correct.06 | Wow, nice move! | B | 2.5 | [ ] |
| 54 | b.correct.07 | Excellent thinking! | B | 2 | [ ] |
| 55 | b.correct.08 | That is it, spot on. Hats off! | B | 4 | [ ] |
| 56 | b.correct.09 | Now that is lovely thinking! | B | 3 | [ ] |
| 57 | b.correct.10 | Yes! That is the right answer. | B | 3.5 | [ ] |
| 58 | b.again.01 | Almost! Try again. | G | 2.5 | [ ] |
| 59 | b.again.02 | Not quite, but you are on the right track. | G | 5 | [ ] |
| 60 | b.again.03 | Hmm, let us take another look. | G | 3.5 | [ ] |

## en-CA session 3 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 61 | b.again.04 | Not yet. Shall we try together? | G | 3.5 | [ ] |
| 62 | b.again.05 | Good try! Give it another go. | G | 3.5 | [ ] |
| 63 | b.again.06 | Not that one, but searching is how we learn. | G | 5 | [ ] |
| 64 | b.again.07 | Oops, a little detour! You can try again. | G | 4.5 | [ ] |
| 65 | b.again.08 | Breathe, read it again, and try once more. | G | 4.5 | [ ] |
| 66 | b.hint.01 | A little nudge: read the question slowly. | H | 4 | [ ] |
| 67 | b.hint.02 | A hint: look for the numbers in the story. | H | 5 | [ ] |
| 68 | b.hint.03 | Try drawing a picture in your head. | H | 4 | [ ] |
| 69 | b.hint.04 | You can count on your fingers, that is allowed! | H | 5 | [ ] |
| 70 | b.hint.05 | Ask yourself: are we adding, or taking away? | H | 4.5 | [ ] |
| 71 | b.hint.06 | Start from a small number and go step by step. | H | 5.5 | [ ] |
| 72 | b.hint.07 | Break the problem into small parts. | H | 3.5 | [ ] |
| 73 | b.encourage.01 | You can do it, I am right here. | G | 4.5 | [ ] |
| 74 | b.encourage.02 | Every try teaches you something. | G | 3 | [ ] |
| 75 | b.encourage.03 | No worries, we will take another path. | G | 4 | [ ] |
| 76 | b.encourage.04 | You are more capable than you think. | G | 4 | [ ] |
| 77 | b.encourage.05 | Here, you are allowed to search. | G | 3.5 | [ ] |
| 78 | b.encourage.06 | I believe in you, really. | G | 3 | [ ] |
| 79 | b.streak.01 | That is several days in a row, hooray! | B | 4.5 | [ ] |
| 80 | b.streak.02 | Your streak goes on, what a great habit! | B | 4.5 | [ ] |
| 81 | b.streak.03 | One more day in your streak! | B | 3.5 | [ ] |
| 82 | b.card.01 | You earned a new card! | B | 3 | [ ] |
| 83 | b.card.02 | A card for your collection, bravo! | B | 3.5 | [ ] |
| 84 | b.card.03 | Look, a card just for you! | B | 3.5 | [ ] |
| 85 | b.chapterpass.01 | Bravo, chapter complete! | B | 2.5 | [ ] |
| 86 | b.chapterpass.02 | Super work, keep it up! | B | 3 | [ ] |
| 87 | b.chapterpass.03 | What a great success! | B | 2.5 | [ ] |
| 88 | b.chapterpass.04 | Excellent, you are making great progress! | B | 3.5 | [ ] |
| 89 | b.chapterpass.05 | Fantastic, the card is yours! | B | 3 | [ ] |
| 90 | b.chapterpass.06 | Hats off, that was lovely work! | B | 3.5 | [ ] |

## en-CA session 4 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 91 | b.chaptermiss.01 | Almost! We will try again together another time. | G | 4.5 | [ ] |
| 92 | b.chaptermiss.02 | Not yet, and that is normal. See you soon for another try! | G | 6 | [ ] |
| 93 | b.chaptermiss.03 | We learn by trying. Come back whenever you like! | G | 5 | [ ] |
| 94 | b.chaptermiss.04 | Keep going! You are nearly there. | G | 3.5 | [ ] |
| 95 | b.detour.01 | A little path to the side. | S | 3.5 | [ ] |
| 96 | b.detour.02 | Look, another road. | S | 2.5 | [ ] |
| 97 | b.detour.03 | We switch trails. | S | 2.5 | [ ] |
| 98 | b.detour.04 | The path bends. | S | 2.5 | [ ] |
| 99 | b.detour.05 | What if we took a turn? | S | 3.5 | [ ] |
| 100 | b.surprise.01 | Oh, a gust of wind! | Q | 3 | [ ] |
| 101 | b.surprise.02 | Here is a twist! | Q | 2.5 | [ ] |
| 102 | b.surprise.03 | Something moved. | Q | 2 | [ ] |
| 103 | b.surprise.04 | Well, something new! | Q | 2.5 | [ ] |
| 104 | b.surprise.05 | The story does a pirouette. | Q | 3 | [ ] |
| 105 | d.world.nature | The Nature world. Explore the forest and its secrets. | W | 5 | [ ] |
| 106 | d.world.espace | The Space world. A mission to the stars. | W | 4.5 | [ ] |
| 107 | d.world.cuisine | The Cooking world. A big meal to cook. | W | 4.5 | [ ] |
| 108 | d.world.sport | The Sport world. The big match is coming. | W | 4.5 | [ ] |
| 109 | d.world.construction | The Building world. Build something amazing. | W | 3.5 | [ ] |
| 110 | d.world.medecine | The Medicine world. Heal and save the day. | W | 4.5 | [ ] |
| 111 | d.world.science | The Science world. The lab needs you. | W | 4 | [ ] |
| 112 | d.world.echecs | The Chess world. A tournament awaits. | W | 3.5 | [ ] |
| 113 | d.world.ocean | The Ocean world. Dive into the reef. | W | 4 | [ ] |
| 114 | d.world.dinosaures | The Dinosaurs world. Giant bones are waiting. | W | 4 | [ ] |
| 115 | d.world.animaux | The Animals world. Feeding time on the farm. | W | 4.5 | [ ] |
| 116 | d.world.musique | The Music world. The concert starts soon. | W | 4 | [ ] |
| 117 | d.world.transport | The Vehicles world. All aboard. | W | 3 | [ ] |
| 118 | d.world.pirates | The Pirates world. A treasure map to follow. | W | 4.5 | [ ] |
| 119 | d.world.marche | The Market world. Count your coins at the market. | W | 5 | [ ] |
| 120 | d.world.meteo | The Weather world. What will the weather be tomorrow?. | W | 5 | [ ] |

## en-CA session 5 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 121 | d.world.robots | The Robots world. Program your robot. | W | 3.5 | [ ] |
| 122 | d.world.arts | The Art studio world. The studio is ready. | W | 4.5 | [ ] |
| 123 | d.world.hiver | The Winter world. The sugar shack is open. | W | 4.5 | [ ] |
| 124 | d.world.jeux | The Game night world. It’s game night. | W | 4 | [ ] |
| 125 | d.chapter.generic.01 | A new chapter begins. | W | 2.5 | [ ] |
| 126 | d.chapter.generic.02 | Here is the next chapter. | W | 3 | [ ] |
| 127 | d.chapter.generic.03 | Ready for the next part of the adventure? | W | 4.5 | [ ] |
| 128 | d.chapter.generic.04 | Let us turn the page. | W | 3 | [ ] |
| 129 | d.chapter.nature.feuilles | Autumn leaves. | W | 2 | [ ] |
| 130 | d.chapter.nature.sentier | The number trail. | W | 2.5 | [ ] |
| 131 | d.chapter.nature.campement | The campsite. | W | 2 | [ ] |
| 132 | d.chapter.nature.observer | Morning birds. | W | 2 | [ ] |
| 133 | d.chapter.nature.rivieres | The streams. | W | 2 | [ ] |
| 134 | d.chapter.espace.decompte | The launch sheet. | W | 2.5 | [ ] |
| 135 | d.chapter.espace.fusee | Fuelling the rocket. | W | 2.5 | [ ] |
| 136 | d.chapter.espace.orbites | The solar panels. | W | 2.5 | [ ] |
| 137 | d.chapter.espace.horloge | The base clock. | W | 2.5 | [ ] |
| 138 | d.chapter.espace.distances | Distances. | W | 1.5 | [ ] |
| 139 | d.chapter.cuisine.fournee | The batch of cookies. | W | 2.5 | [ ] |
| 140 | d.chapter.cuisine.recette | The pizza recipe. | W | 2.5 | [ ] |
| 141 | d.chapter.cuisine.mesures | Recipe measures. | W | 2 | [ ] |
| 142 | d.chapter.cuisine.partage | The big share. | W | 2.5 | [ ] |
| 143 | d.chapter.cuisine.tablee | The big table. | W | 2.5 | [ ] |
| 144 | d.chapter.sport.points | Match points. | W | 2 | [ ] |
| 145 | d.chapter.sport.classement | The results chart. | W | 2.5 | [ ] |
| 146 | d.chapter.sport.relais | The relay. | W | 2 | [ ] |
| 147 | d.chapter.sport.chrono | Training session. | W | 2 | [ ] |
| 148 | d.chapter.sport.tournoi | The draw. | W | 2 | [ ] |
| 149 | d.chapter.construction.formes | The treehouse blueprints. | W | 2.5 | [ ] |
| 150 | d.chapter.construction.solides | The building blocks. | W | 2.5 | [ ] |

## en-CA session 6 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 151 | d.chapter.construction.perimetre | Around the floor. | W | 2.5 | [ ] |
| 152 | d.chapter.construction.surface | The wall to paint. | W | 2.5 | [ ] |
| 153 | d.chapter.construction.regles | The ruler and the metre stick. | W | 3.5 | [ ] |
| 154 | d.chapter.medecine.doses | Clinic doses. | W | 2 | [ ] |
| 155 | d.chapter.medecine.poids | Weighing the patients. | W | 2.5 | [ ] |
| 156 | d.chapter.medecine.horaire | Check-up times. | W | 2 | [ ] |
| 157 | d.chapter.medecine.dossier | The visit records. | W | 2.5 | [ ] |
| 158 | d.chapter.medecine.pansements | Boxes of bandages. | W | 2.5 | [ ] |
| 159 | d.chapter.science.mesurer | Measuring in the lab. | W | 2.5 | [ ] |
| 160 | d.chapter.science.hasard | The game of chance. | W | 2.5 | [ ] |
| 161 | d.chapter.science.resultats | Experiment results. | W | 2 | [ ] |
| 162 | d.chapter.science.suites | Measurement sequences. | W | 2 | [ ] |
| 163 | d.chapter.science.melanges | The mixtures. | W | 2 | [ ] |
| 164 | d.chapter.echecs.cases | The board squares. | W | 2.5 | [ ] |
| 165 | d.chapter.echecs.parcours | The knight’s path. | W | 2.5 | [ ] |
| 166 | d.chapter.echecs.valeurs | Piece values. | W | 2 | [ ] |
| 167 | d.chapter.echecs.motifs | Checkerboard patterns. | W | 2 | [ ] |
| 168 | d.chapter.echecs.partie | The game. | W | 2 | [ ] |
| 169 | d.chapter.ocean.recif | Reef fish. | W | 2 | [ ] |
| 170 | d.chapter.ocean.profondeur | The dive. | W | 2 | [ ] |
| 171 | d.chapter.ocean.baleines | Whales of the St. Lawrence. | W | 3 | [ ] |
| 172 | d.chapter.ocean.sous-marin | The submarine. | W | 2 | [ ] |
| 173 | d.chapter.dinosaures.fossiles | Fossils. | W | 1.5 | [ ] |
| 174 | d.chapter.dinosaures.os | Skeleton bones. | W | 2 | [ ] |
| 175 | d.chapter.dinosaures.geants | The giants. | W | 2 | [ ] |
| 176 | d.chapter.dinosaures.oeufs | The egg nests. | W | 2.5 | [ ] |
| 177 | d.chapter.animaux.ferme | The farm. | W | 2 | [ ] |
| 178 | d.chapter.animaux.pattes | Paws and legs. | W | 2.5 | [ ] |
| 179 | d.chapter.animaux.zoo | The zoo. | W | 2 | [ ] |
| 180 | d.chapter.animaux.pairs | Even or odd. | W | 2.5 | [ ] |

## en-CA session 7 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 181 | d.chapter.musique.rythme | The rhythm. | W | 2 | [ ] |
| 182 | d.chapter.musique.mesures | Bars of the score. | W | 2.5 | [ ] |
| 183 | d.chapter.musique.concert | The concert. | W | 2 | [ ] |
| 184 | d.chapter.musique.instruments | The instruments. | W | 2 | [ ] |
| 185 | d.chapter.transport.gare | The station. | W | 2 | [ ] |
| 186 | d.chapter.transport.autobus | The school bus. | W | 2.5 | [ ] |
| 187 | d.chapter.transport.pistes | The tracks. | W | 2 | [ ] |
| 188 | d.chapter.transport.voyage | The long trip. | W | 2.5 | [ ] |
| 189 | d.chapter.pirates.carte | The treasure map. | W | 2.5 | [ ] |
| 190 | d.chapter.pirates.tresor | The treasure chest. | W | 2.5 | [ ] |
| 191 | d.chapter.pirates.butin | Sharing the loot. | W | 2.5 | [ ] |
| 192 | d.chapter.pirates.navire | The ship. | W | 2 | [ ] |
| 193 | d.chapter.marche.pieces | Coins. | W | 1.5 | [ ] |
| 194 | d.chapter.marche.prix | Prices. | W | 1.5 | [ ] |
| 195 | d.chapter.marche.monnaie | Change. | W | 1.5 | [ ] |
| 196 | d.chapter.marche.panier | The basket. | W | 2 | [ ] |
| 197 | d.chapter.marche.economies | Savings. | W | 1.5 | [ ] |
| 198 | d.chapter.meteo.pluie | The rain. | W | 2 | [ ] |
| 199 | d.chapter.meteo.temperature | Temperature. | W | 1.5 | [ ] |
| 200 | d.chapter.meteo.prevision | The forecast. | W | 2 | [ ] |
| 201 | d.chapter.meteo.saisons | The seasons. | W | 2 | [ ] |
| 202 | d.chapter.robots.programme | The program. | W | 2 | [ ] |
| 203 | d.chapter.robots.grille | The grid. | W | 2 | [ ] |
| 204 | d.chapter.robots.miroir | The mirror robot. | W | 2.5 | [ ] |
| 205 | d.chapter.robots.boucles | Loops. | W | 1.5 | [ ] |
| 206 | d.chapter.arts.formes | Shapes. | W | 1.5 | [ ] |
| 207 | d.chapter.arts.frises | Friezes. | W | 1.5 | [ ] |
| 208 | d.chapter.arts.mosaique | The mosaic. | W | 2 | [ ] |
| 209 | d.chapter.arts.symetrie | Lines in the studio. | W | 2.5 | [ ] |
| 210 | d.chapter.hiver.erabliere | The sugar bush. | W | 2.5 | [ ] |

## en-CA session 8 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 211 | d.chapter.hiver.patinoire | The rink. | W | 2 | [ ] |
| 212 | d.chapter.hiver.neige | Snow. | W | 1.5 | [ ] |
| 213 | d.chapter.hiver.horaire | The winter village schedule. | W | 2.5 | [ ] |
| 214 | d.chapter.jeux.des | Dice. | W | 1.5 | [ ] |
| 215 | d.chapter.jeux.cartes | Cards. | W | 1.5 | [ ] |
| 216 | d.chapter.jeux.points | Points. | W | 1.5 | [ ] |
| 217 | d.chapter.jeux.chance | Luck. | W | 1.5 | [ ] |
| 218 | f.gen.open.01 | A new adventure begins, and you are part of it. | S | 5.5 | [ ] |
| 219 | f.gen.open.02 | Here is a new path to explore. | S | 4 | [ ] |
| 220 | f.gen.open.03 | Someone needs your help! | S | 2.5 | [ ] |
| 221 | f.gen.ok.01 | Well done! The story can go on. | S | 4 | [ ] |
| 222 | f.gen.ok.02 | The count is right, and everybody is happy. | S | 4.5 | [ ] |
| 223 | f.gen.ok.03 | Bravo! You found the right path. | S | 3.5 | [ ] |
| 224 | f.gen.dt.01 | It is not quite right, but you tried well. Take your time. | S | 6 | [ ] |
| 225 | f.gen.dt.02 | A little detour, nothing serious. We try again whenever you like. | S | 5.5 | [ ] |
| 226 | f.gen.dt.03 | Not this time, and that is normal. We learn by trying. | S | 5.5 | [ ] |
| 227 | f.gen.su.01 | Oh, the story takes a surprise turn! | S | 4 | [ ] |
| 228 | f.gen.su.02 | Well, nobody saw that coming! | S | 3 | [ ] |
| 229 | f.gen.su.03 | What a funny surprise! | S | 2.5 | [ ] |
| 230 | f.nature.open | In the forest, something stirs among the leaves. | S | 4.5 | [ ] |
| 231 | f.nature.ok | The count is right, and the forest is glad. | S | 5 | [ ] |
| 232 | f.nature.dt | The count is not quite right. The forest is waiting for another try. | S | 6.5 | [ ] |
| 233 | f.nature.su | A gust of wind, and the leaves start to dance! | S | 5.5 | [ ] |
| 234 | f.espace.open | Up high, the stars twinkle. The mission can begin. | S | 5 | [ ] |
| 235 | f.espace.ok | Well worked out! The rocket follows the right path. | S | 5 | [ ] |
| 236 | f.espace.dt | The rocket drifts a little. We can fix the course. | S | 5.5 | [ ] |
| 237 | f.espace.su | A comet zooms by, what a sight! | S | 4 | [ ] |
| 238 | f.cuisine.open | It smells wonderful in the kitchen, a big meal is on the way. | S | 6.5 | [ ] |
| 239 | f.cuisine.ok | The recipe works, and it smells even better. | S | 4.5 | [ ] |
| 240 | f.cuisine.dt | The amounts are not quite right. We taste and adjust. | S | 5.5 | [ ] |

## en-CA session 9 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 241 | f.cuisine.su | Poof! A cloud of flour floats up in the room. | S | 5.5 | [ ] |
| 242 | f.sport.open | The stadium is full, the big match is near. | S | 5 | [ ] |
| 243 | f.sport.ok | What a move! The ball flies straight to the goal. | S | 5.5 | [ ] |
| 244 | f.sport.dt | The ball brushes the post. We get ready for another shot. | S | 5.5 | [ ] |
| 245 | f.sport.su | An unexpected bounce, and the crowd holds its breath! | S | 5 | [ ] |
| 246 | f.construction.open | The building site wakes up, there is a bridge to build. | S | 5.5 | [ ] |
| 247 | f.construction.ok | Well worked out! The structure is solid. | S | 4 | [ ] |
| 248 | f.construction.dt | One beam is a bit too short. We measure again and start over. | S | 6.5 | [ ] |
| 249 | f.construction.su | A crane swings around and drops off a surprise! | S | 5 | [ ] |
| 250 | f.medecine.open | At the clinic, someone needs your help. | S | 4 | [ ] |
| 251 | f.medecine.ok | The right calculation helps, and everyone feels better. | S | 4.5 | [ ] |
| 252 | f.medecine.dt | The amount is not quite right. We check together, gently. | S | 5.5 | [ ] |
| 253 | f.medecine.su | A little laugh rings out in the waiting room! | S | 5 | [ ] |
| 254 | f.science.open | In the lab, the test tubes glow and the experiment begins. | S | 5.5 | [ ] |
| 255 | f.science.ok | The mix is right, and the flask bubbles! | S | 4.5 | [ ] |
| 256 | f.science.dt | The reaction fizzles out a little. We run the experiment again. | S | 5.5 | [ ] |
| 257 | f.science.su | Psssh! A cloud of coloured smoke puffs out of the flask. | S | 5.5 | [ ] |
| 258 | f.echecs.open | The board is ready, the pieces await your plan. | S | 5 | [ ] |
| 259 | f.echecs.ok | Beautiful move! Your plan is going well. | S | 4 | [ ] |
| 260 | f.echecs.dt | The knight is stuck. There is always another path. | S | 5 | [ ] |
| 261 | f.echecs.su | A pawn steps forward when nobody expected it! | S | 4.5 | [ ] |
| 262 | f.ocean.open | Beneath the waves, a world of colour awaits. | S | 4.5 | [ ] |
| 263 | f.ocean.ok | Well counted! The fish dance around you. | S | 4 | [ ] |
| 264 | f.ocean.dt | The current pushes us aside a bit. We swim again. | S | 5.5 | [ ] |
| 265 | f.ocean.su | A giant bubble drifts by and makes the fish giggle! | S | 5.5 | [ ] |
| 266 | f.dinosaures.open | The dinosaur valley is waking up slowly. | S | 4 | [ ] |
| 267 | f.dinosaures.ok | The count is right, and a little dinosaur leaps for joy! | S | 5.5 | [ ] |
| 268 | f.dinosaures.dt | The tracks are not quite counted yet. Let us look closer. | S | 5.5 | [ ] |
| 269 | f.dinosaures.su | Boom, boom! Big footsteps make the ground shake. | S | 4.5 | [ ] |
| 270 | f.animaux.open | On the farm, the animals are eagerly waiting for you. | S | 5.5 | [ ] |

## en-CA session 10 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 271 | f.animaux.ok | Well counted! Everybody is happy. | S | 3 | [ ] |
| 272 | f.animaux.dt | One little critter is missing from the count. Let us count again. | S | 6 | [ ] |
| 273 | f.animaux.su | Cluck cluck! A hen starts singing very loudly. | S | 4.5 | [ ] |
| 274 | f.musique.open | The instruments tune up, the concert is about to begin. | S | 5.5 | [ ] |
| 275 | f.musique.ok | What a lovely beat! The melody is perfect. | S | 4.5 | [ ] |
| 276 | f.musique.dt | One note sounds a little off. We start the bar again. | S | 5.5 | [ ] |
| 277 | f.musique.su | Pom pom pom! The drum plays a surprise beat. | S | 5 | [ ] |
| 278 | f.transport.open | The engines hum, a big trip begins. | S | 4 | [ ] |
| 279 | f.transport.ok | Well figured! The trip goes wonderfully. | S | 3.5 | [ ] |
| 280 | f.transport.dt | We took a little detour. No worries, off we go again. | S | 5.5 | [ ] |
| 281 | f.transport.su | Toot toot! A train whistles in the distance. | S | 4.5 | [ ] |
| 282 | f.pirates.open | The ship rocks, a treasure map awaits you. | S | 4.5 | [ ] |
| 283 | f.pirates.ok | Well counted! The chest creaks open. | S | 3.5 | [ ] |
| 284 | f.pirates.dt | The count is not right. The map is waiting for another try. | S | 6 | [ ] |
| 285 | f.pirates.su | A parrot squawks a secret from the mast! | S | 4.5 | [ ] |
| 286 | f.marche.open | At the market, the stalls are full of colour and good smells. | S | 6 | [ ] |
| 287 | f.marche.ok | The count is right, and the vendor smiles. | S | 4.5 | [ ] |
| 288 | f.marche.dt | The change is not quite right. We count the coins again. | S | 5.5 | [ ] |
| 289 | f.marche.su | An apple rolls off the stall, quick, catch it! | S | 5 | [ ] |
| 290 | f.meteo.open | The sky is changing, the clouds are gathering. | S | 4.5 | [ ] |
| 291 | f.meteo.ok | Well read! The forecast is right. | S | 3.5 | [ ] |
| 292 | f.meteo.dt | The forecast is not quite right. Let us check the thermometer again. | S | 6 | [ ] |
| 293 | f.meteo.su | A rainbow appears right after the rain! | S | 4 | [ ] |
| 294 | f.robots.open | In the workshop, the robots hum and blink. | S | 4.5 | [ ] |
| 295 | f.robots.ok | Calculation done! The robot does a little dance. | S | 4.5 | [ ] |
| 296 | f.robots.dt | The robot has a tiny glitch. We fix it together. | S | 5.5 | [ ] |
| 297 | f.robots.su | Beep boop! The robot pulls out a magic trick. | S | 5 | [ ] |
| 298 | f.arts.open | In the studio, the brushes are ready and the canvas is blank. | S | 6 | [ ] |
| 299 | f.arts.ok | Well measured! The colours match beautifully. | S | 3.5 | [ ] |
| 300 | f.arts.dt | The colours are not quite mixed right. We blend again. | S | 5.5 | [ ] |

## en-CA session 11 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 301 | f.arts.su | Splat! A drop of paint falls and makes a star. | S | 5.5 | [ ] |
| 302 | f.hiver.open | Snow falls softly, everything is white and calm. | S | 4.5 | [ ] |
| 303 | f.hiver.ok | Well counted! The snowman is smiling. | S | 3.5 | [ ] |
| 304 | f.hiver.dt | A few snowballs are missing. We roll some more. | S | 5 | [ ] |
| 305 | f.hiver.su | Whoosh! The snow starts swirling around you. | S | 4 | [ ] |
| 306 | f.jeux.open | Game night begins, the dice are ready. | S | 4 | [ ] |
| 307 | f.jeux.ok | Nice play! The points add up nicely. | S | 4 | [ ] |
| 308 | f.jeux.dt | The total is not quite right. We replay the round. | S | 5.5 | [ ] |
| 309 | f.jeux.su | The dice roll, and it is a double! | S | 4.5 | [ ] |
| 310 | c.atom.en.01 | zero | N | 1.5 | [ ] |
| 311 | c.atom.en.02 | one | N | 1.5 | [ ] |
| 312 | c.atom.en.03 | two | N | 1.5 | [ ] |
| 313 | c.atom.en.04 | three | N | 1.5 | [ ] |
| 314 | c.atom.en.05 | four | N | 1.5 | [ ] |
| 315 | c.atom.en.06 | five | N | 1.5 | [ ] |
| 316 | c.atom.en.07 | six | N | 1.5 | [ ] |
| 317 | c.atom.en.08 | seven | N | 1.5 | [ ] |
| 318 | c.atom.en.09 | eight | N | 1.5 | [ ] |
| 319 | c.atom.en.10 | nine | N | 1.5 | [ ] |
| 320 | c.atom.en.11 | ten | N | 1.5 | [ ] |
| 321 | c.atom.en.12 | eleven | N | 1.5 | [ ] |
| 322 | c.atom.en.13 | twelve | N | 1.5 | [ ] |
| 323 | c.atom.en.14 | thirteen | N | 1.5 | [ ] |
| 324 | c.atom.en.15 | fourteen | N | 1.5 | [ ] |
| 325 | c.atom.en.16 | fifteen | N | 1.5 | [ ] |
| 326 | c.atom.en.17 | sixteen | N | 1.5 | [ ] |
| 327 | c.atom.en.18 | seventeen | N | 1.5 | [ ] |
| 328 | c.atom.en.19 | eighteen | N | 1.5 | [ ] |
| 329 | c.atom.en.20 | nineteen | N | 1.5 | [ ] |
| 330 | c.atom.en.21 | twenty | N | 1.5 | [ ] |

## en-CA session 12 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 331 | c.atom.en.22 | thirty | N | 1.5 | [ ] |
| 332 | c.atom.en.23 | forty | N | 1.5 | [ ] |
| 333 | c.atom.en.24 | fifty | N | 1.5 | [ ] |
| 334 | c.atom.en.25 | sixty | N | 1.5 | [ ] |
| 335 | c.atom.en.26 | seventy | N | 1.5 | [ ] |
| 336 | c.atom.en.27 | eighty | N | 1.5 | [ ] |
| 337 | c.atom.en.28 | ninety | N | 1.5 | [ ] |
| 338 | c.atom.en.29 | hundred | N | 1.5 | [ ] |
| 339 | c.atom.en.30 | thousand | N | 1.5 | [ ] |
| 340 | c.atom.en.31 | million | N | 1.5 | [ ] |
| 341 | c.atom.en.32 | and | N | 1.5 | [ ] |
| 342 | c.atom.en.33 | point | N | 1.5 | [ ] |
| 343 | c.num.000 | zero (x2) | N | 1.5 | [ ] |
| 344 | c.num.001 | one (x2) | N | 1.5 | [ ] |
| 345 | c.num.002 | two (x2) | N | 1.5 | [ ] |
| 346 | c.num.003 | three (x2) | N | 1.5 | [ ] |
| 347 | c.num.004 | four (x2) | N | 1.5 | [ ] |
| 348 | c.num.005 | five (x2) | N | 1.5 | [ ] |
| 349 | c.num.006 | six (x2) | N | 1.5 | [ ] |
| 350 | c.num.007 | seven (x2) | N | 1.5 | [ ] |
| 351 | c.num.008 | eight (x2) | N | 1.5 | [ ] |
| 352 | c.num.009 | nine (x2) | N | 1.5 | [ ] |
| 353 | c.num.010 | ten (x2) | N | 1.5 | [ ] |
| 354 | c.num.011 | eleven (x2) | N | 1.5 | [ ] |
| 355 | c.num.012 | twelve (x2) | N | 1.5 | [ ] |
| 356 | c.num.013 | thirteen (x2) | N | 1.5 | [ ] |
| 357 | c.num.014 | fourteen (x2) | N | 1.5 | [ ] |
| 358 | c.num.015 | fifteen (x2) | N | 1.5 | [ ] |
| 359 | c.num.016 | sixteen (x2) | N | 1.5 | [ ] |
| 360 | c.num.017 | seventeen (x2) | N | 1.5 | [ ] |

## en-CA session 13 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 361 | c.num.018 | eighteen (x2) | N | 1.5 | [ ] |
| 362 | c.num.019 | nineteen (x2) | N | 1.5 | [ ] |
| 363 | c.num.020 | twenty (x2) | N | 1.5 | [ ] |
| 364 | c.num.021 | twenty-one (x2) | N | 1.5 | [ ] |
| 365 | c.num.022 | twenty-two (x2) | N | 1.5 | [ ] |
| 366 | c.num.023 | twenty-three (x2) | N | 1.5 | [ ] |
| 367 | c.num.024 | twenty-four (x2) | N | 1.5 | [ ] |
| 368 | c.num.025 | twenty-five (x2) | N | 1.5 | [ ] |
| 369 | c.num.026 | twenty-six (x2) | N | 1.5 | [ ] |
| 370 | c.num.027 | twenty-seven (x2) | N | 1.5 | [ ] |
| 371 | c.num.028 | twenty-eight (x2) | N | 1.5 | [ ] |
| 372 | c.num.029 | twenty-nine (x2) | N | 1.5 | [ ] |
| 373 | c.num.030 | thirty (x2) | N | 1.5 | [ ] |
| 374 | c.num.031 | thirty-one (x2) | N | 1.5 | [ ] |
| 375 | c.num.032 | thirty-two (x2) | N | 1.5 | [ ] |
| 376 | c.num.033 | thirty-three (x2) | N | 1.5 | [ ] |
| 377 | c.num.034 | thirty-four (x2) | N | 1.5 | [ ] |
| 378 | c.num.035 | thirty-five (x2) | N | 1.5 | [ ] |
| 379 | c.num.036 | thirty-six (x2) | N | 1.5 | [ ] |
| 380 | c.num.037 | thirty-seven (x2) | N | 1.5 | [ ] |
| 381 | c.num.038 | thirty-eight (x2) | N | 1.5 | [ ] |
| 382 | c.num.039 | thirty-nine (x2) | N | 1.5 | [ ] |
| 383 | c.num.040 | forty (x2) | N | 1.5 | [ ] |
| 384 | c.num.041 | forty-one (x2) | N | 1.5 | [ ] |
| 385 | c.num.042 | forty-two (x2) | N | 1.5 | [ ] |
| 386 | c.num.043 | forty-three (x2) | N | 1.5 | [ ] |
| 387 | c.num.044 | forty-four (x2) | N | 1.5 | [ ] |
| 388 | c.num.045 | forty-five (x2) | N | 1.5 | [ ] |
| 389 | c.num.046 | forty-six (x2) | N | 1.5 | [ ] |
| 390 | c.num.047 | forty-seven (x2) | N | 1.5 | [ ] |

## en-CA session 14 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 391 | c.num.048 | forty-eight (x2) | N | 1.5 | [ ] |
| 392 | c.num.049 | forty-nine (x2) | N | 1.5 | [ ] |
| 393 | c.num.050 | fifty (x2) | N | 1.5 | [ ] |
| 394 | c.num.051 | fifty-one (x2) | N | 1.5 | [ ] |
| 395 | c.num.052 | fifty-two (x2) | N | 1.5 | [ ] |
| 396 | c.num.053 | fifty-three (x2) | N | 1.5 | [ ] |
| 397 | c.num.054 | fifty-four (x2) | N | 1.5 | [ ] |
| 398 | c.num.055 | fifty-five (x2) | N | 1.5 | [ ] |
| 399 | c.num.056 | fifty-six (x2) | N | 1.5 | [ ] |
| 400 | c.num.057 | fifty-seven (x2) | N | 1.5 | [ ] |
| 401 | c.num.058 | fifty-eight (x2) | N | 1.5 | [ ] |
| 402 | c.num.059 | fifty-nine (x2) | N | 1.5 | [ ] |
| 403 | c.num.060 | sixty (x2) | N | 1.5 | [ ] |
| 404 | c.num.061 | sixty-one (x2) | N | 1.5 | [ ] |
| 405 | c.num.062 | sixty-two (x2) | N | 1.5 | [ ] |
| 406 | c.num.063 | sixty-three (x2) | N | 1.5 | [ ] |
| 407 | c.num.064 | sixty-four (x2) | N | 1.5 | [ ] |
| 408 | c.num.065 | sixty-five (x2) | N | 1.5 | [ ] |
| 409 | c.num.066 | sixty-six (x2) | N | 1.5 | [ ] |
| 410 | c.num.067 | sixty-seven (x2) | N | 1.5 | [ ] |
| 411 | c.num.068 | sixty-eight (x2) | N | 1.5 | [ ] |
| 412 | c.num.069 | sixty-nine (x2) | N | 1.5 | [ ] |
| 413 | c.num.070 | seventy (x2) | N | 1.5 | [ ] |
| 414 | c.num.071 | seventy-one (x2) | N | 1.5 | [ ] |
| 415 | c.num.072 | seventy-two (x2) | N | 1.5 | [ ] |
| 416 | c.num.073 | seventy-three (x2) | N | 1.5 | [ ] |
| 417 | c.num.074 | seventy-four (x2) | N | 1.5 | [ ] |
| 418 | c.num.075 | seventy-five (x2) | N | 1.5 | [ ] |
| 419 | c.num.076 | seventy-six (x2) | N | 1.5 | [ ] |
| 420 | c.num.077 | seventy-seven (x2) | N | 1.5 | [ ] |

## en-CA session 15 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 421 | c.num.078 | seventy-eight (x2) | N | 1.5 | [ ] |
| 422 | c.num.079 | seventy-nine (x2) | N | 1.5 | [ ] |
| 423 | c.num.080 | eighty (x2) | N | 1.5 | [ ] |
| 424 | c.num.081 | eighty-one (x2) | N | 1.5 | [ ] |
| 425 | c.num.082 | eighty-two (x2) | N | 1.5 | [ ] |
| 426 | c.num.083 | eighty-three (x2) | N | 1.5 | [ ] |
| 427 | c.num.084 | eighty-four (x2) | N | 1.5 | [ ] |
| 428 | c.num.085 | eighty-five (x2) | N | 1.5 | [ ] |
| 429 | c.num.086 | eighty-six (x2) | N | 1.5 | [ ] |
| 430 | c.num.087 | eighty-seven (x2) | N | 1.5 | [ ] |
| 431 | c.num.088 | eighty-eight (x2) | N | 1.5 | [ ] |
| 432 | c.num.089 | eighty-nine (x2) | N | 1.5 | [ ] |
| 433 | c.num.090 | ninety (x2) | N | 1.5 | [ ] |
| 434 | c.num.091 | ninety-one (x2) | N | 1.5 | [ ] |
| 435 | c.num.092 | ninety-two (x2) | N | 1.5 | [ ] |
| 436 | c.num.093 | ninety-three (x2) | N | 1.5 | [ ] |
| 437 | c.num.094 | ninety-four (x2) | N | 1.5 | [ ] |
| 438 | c.num.095 | ninety-five (x2) | N | 1.5 | [ ] |
| 439 | c.num.096 | ninety-six (x2) | N | 1.5 | [ ] |
| 440 | c.num.097 | ninety-seven (x2) | N | 1.5 | [ ] |
| 441 | c.num.098 | ninety-eight (x2) | N | 1.5 | [ ] |
| 442 | c.num.099 | ninety-nine (x2) | N | 1.5 | [ ] |
| 443 | c.num.100 | one hundred (x2) | N | 2 | [ ] |
| 444 | c.unit.cm | centimetre | N | 1.5 | [ ] |
| 445 | c.unit.m | metre | N | 1.5 | [ ] |
| 446 | c.unit.km | kilometre | N | 1.5 | [ ] |
| 447 | c.unit.g | gram | N | 1.5 | [ ] |
| 448 | c.unit.kg | kilogram | N | 1.5 | [ ] |
| 449 | c.unit.l | litre | N | 1.5 | [ ] |
| 450 | c.unit.ml | millilitre | N | 1.5 | [ ] |

## en-CA session 16 (30 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 451 | c.unit.s | second | N | 1.5 | [ ] |
| 452 | c.unit.min | minute | N | 1.5 | [ ] |
| 453 | c.unit.h | hour | N | 1.5 | [ ] |
| 454 | c.unit.day | day | N | 1.5 | [ ] |
| 455 | c.unit.week | week | N | 1.5 | [ ] |
| 456 | c.unit.month | month | N | 1.5 | [ ] |
| 457 | c.unit.year | year | N | 1.5 | [ ] |
| 458 | c.unit.dollar | dollar | N | 1.5 | [ ] |
| 459 | c.unit.cent | cent | N | 1.5 | [ ] |
| 460 | c.unit.deg | degree | N | 1.5 | [ ] |
| 461 | c.unit.pct | percent | N | 1.5 | [ ] |
| 462 | c.unit.sq | squared | N | 1.5 | [ ] |
| 463 | c.unit.unit | unit | N | 1.5 | [ ] |
| 464 | c.op.plus | plus | N | 1.5 | [ ] |
| 465 | c.op.minus | minus | N | 1.5 | [ ] |
| 466 | c.op.times | times | N | 1.5 | [ ] |
| 467 | c.op.div | divided by | N | 2 | [ ] |
| 468 | c.op.eq | equals | N | 1.5 | [ ] |
| 469 | c.op.howmany | how many | N | 2 | [ ] |
| 470 | c.op.half | half | N | 1.5 | [ ] |
| 471 | c.op.third | third | N | 1.5 | [ ] |
| 472 | c.op.quarter | quarter | N | 1.5 | [ ] |
| 473 | c.op.over | out of | N | 2 | [ ] |
| 474 | c.op.total | in all | N | 2 | [ ] |
| 475 | c.op.more | more | N | 1.5 | [ ] |
| 476 | c.op.less | fewer | N | 1.5 | [ ] |
| 477 | c.op.left | are left | N | 2 | [ ] |
| 478 | c.op.each | each | N | 1.5 | [ ] |
| 479 | c.op.gt | greater than | N | 2 | [ ] |
| 480 | c.op.lt | less than | N | 2 | [ ] |

## en-CA session 17 (1 lines)

| # | Line id | Text | Dir | Max s | OK |
|---|---|---|---|---|---|
| 481 | c.op.eqto | equal to | N | 2 | [ ] |
