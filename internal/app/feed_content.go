// Code generated from frontend/mock/lib/feed.mjs content tables; the texts are product copy, edit them freely.
//
// Go for Spring people: [4]string is a fixed-size array (title fr, text fr, title en, text en); [][4]string is a
// slice (a growable list) of them. Package-level vars are like static final constants of a class.
package app

// feedStories holds the story seeds: title fr, text fr, title en, text en.
var feedStories = [][4]string{
	{"La porte à chiffres", "Un petit renard trouve une porte qui s’ouvre seulement avec le bon calcul. {iW}, chaque réponse allume une lanterne. Il en faut huit pour passer, et il en a déjà trois.", "The door of numbers", "A little fox finds a door that opens only with the right sum. {iW}, every answer lights a lantern. It takes eight to get through, and he already has three."},
	{"La tour qui comptait à l’envers", "{iW}, une tour compte à rebours depuis cent. À chaque marche, elle perd sept. Elle commence à avoir le vertige, la pauvre. Où va-t-elle s’arrêter, d’après toi ?", "The tower that counted backwards", "{iW}, a tower counts down from a hundred. Every step it loses seven. It is getting dizzy, poor thing. Where do you think it will stop?"},
	{"Le marché des moitiés", "Au marché {dW}, tout se partage en deux : une pomme, une carte, même un secret. Ce soir, il faut partager vingt-quatre trésors en parts égales. Qui aura quoi ?", "The market of halves", "At the market {dW}, everything is split in two: an apple, a map, even a secret. Tonight, twenty-four treasures must be shared equally. Who gets what?"},
	{"Le dragon qui aimait les tables", "Un dragon {dW} refuse de dormir sans réciter sa table de huit. Il s’arrête toujours au milieu et recommence en bâillant. Peux-tu l’aider avant que la lune se lève ?", "The dragon who loved times tables", "A dragon {dW} refuses to sleep before reciting the eight times table. He always stops in the middle and starts over, yawning. Can you help before the moon rises?"},
	{"Les trois cent quarante invités", "Trois cent quarante invités arrivent {iW}, et il n’y a que sept grandes tables. Le cuisinier panique un peu. Combien par table, et qui reste debout ?", "The three hundred forty guests", "Three hundred forty guests arrive {iW}, and there are only seven big tables. The cook is panicking a little. How many per table, and who is left standing?"},
	{"La carte aux nombres cachés", "Une vieille carte {dW} cache des nombres sous la mousse. Chaque nombre trouvé double la lumière de la lampe du voyageur. Il en a repéré deux, il en cherche un troisième.", "The map of hidden numbers", "An old map {dW} hides numbers under the moss. Every number found doubles the light of the traveler’s lamp. He has spotted two and is looking for a third."},
	{"La chaussette qui comptait les pas", "{iW}, une chaussette orange a décidé de compter tous les pas de la journée. Elle en est à quatre mille, et elle a perdu sa jumelle quelque part sur le chemin.", "The sock that counted steps", "{iW}, an orange sock decided to count every step of the day. It is up to four thousand, and it lost its twin somewhere along the way."},
	{"L’horloge qui avançait de cinq", "L’horloge {dW} avance de cinq minutes chaque heure. Personne ne comprend pourquoi tout le monde arrive en avance au goûter. Le gardien soupçonne une souris très pressée.", "The clock that ran five ahead", "The clock {dW} runs five minutes fast every hour. Nobody understands why everyone arrives early for snack time. The keeper suspects a very hurried mouse."},
	{"Le jardin aux fleurs doubles", "{iW}, chaque fleur en fait pousser deux le lendemain. Lundi il y en a une, mardi deux, mercredi quatre. Le jardinier se demande où mettre le vase de jeudi.", "The garden of double flowers", "{iW}, every flower grows two more the next day. Monday there is one, Tuesday two, Wednesday four. The gardener wonders where to put Thursday’s vase."},
	{"Le pirate des petites sommes", "Un pirate {dW} ne vole que de toutes petites sommes : trois pièces ici, quatre là. Il note tout dans son carnet. À la fin de la semaine, il a presque un trésor.", "The pirate of small sums", "A pirate {dW} only steals tiny amounts: three coins here, four there. He writes it all in his notebook. By the end of the week, he almost has a treasure."},
	{"Le train des nombres pairs", "Le train {dW} ne s’arrête que dans les gares portant un nombre pair. Les passagers des gares impaires font des signes tristes. Le chef de gare cherche une solution.", "The train of even numbers", "The train {dW} only stops at stations with an even number. Passengers at the odd stations wave sadly. The station master is looking for a solution."},
	{"Le bonhomme aux boutons", "{iW}, un bonhomme de neige a toujours deux fois plus de boutons que de boules. Aujourd’hui il en réclame douze, et il tape du pied. Combien de boules lui faut-il ?", "The snowman with buttons", "{iW}, a snowman always has twice as many buttons as snowballs. Today he demands twelve, and he stamps his foot. How many snowballs does he need?"},
	{"La bibliothèque des pages", "La bibliothèque {dW} classe ses livres par nombre de pages. Le plus mince en a dix, le plus épais en a cent. Un lecteur curieux cherche celui qui se trouve pile au milieu.", "The library of pages", "The library {dW} sorts its books by number of pages. The thinnest has ten, the thickest has a hundred. A curious reader is looking for the one right in the middle."},
	{"L’échelle des dizaines", "Une échelle {dW} a des barreaux comptés de dix en dix. Un écureuil grimpe et compte à voix haute. Il se trompe toujours à soixante, son numéro porte-bonheur.", "The ladder of tens", "A ladder {dW} has rungs counted by tens. A squirrel climbs and counts out loud. He always slips up at sixty, his lucky number."},
	{"Le gâteau des quatre parts", "Pour l’anniversaire du dragon {dW}, on coupe le gâteau en quatre parts égales. Un invité surprise arrive juste après. Il faudra recouper, mais comment ?", "The cake in four parts", "For the birthday of the dragon {dW}, the cake is cut into four equal parts. A surprise guest arrives right after. It will need cutting again, but how?"},
	{"Le facteur aux lettres numérotées", "Le facteur {dW} distribue des lettres numérotées de un à cent. Il n’en reste qu’une, la quarante-deux, sans adresse. Il la glisse dans sa poche en souriant.", "The mailman with numbered letters", "The mailman {dW} delivers letters numbered from one to a hundred. Only one is left, number forty-two, with no address. He slips it into his pocket, smiling."},
	{"Les étoiles du soir", "Chaque soir, {W} allume une étoile de plus que la veille. Hier, il y en avait dix-neuf. Les enfants du village comptent depuis leur fenêtre en chuchotant.", "The evening stars", "Every evening, {W} lights one more star than the night before. Yesterday there were nineteen. The village children count from their windows, whispering."},
	{"Le sac sans fond", "{iW}, un sac sans fond avale tout ce qu’on lui donne : deux billes, puis quatre, puis huit. Il rote doucement. Que va-t-il avaler ensuite, à ton avis ?", "The bottomless bag", "{iW}, a bottomless bag swallows everything it is given: two marbles, then four, then eight. It burps softly. What do you think it will swallow next?"},
}

// feedFacts holds the fun-fact seeds: title fr, text fr, title en, text en.
var feedFacts = [][4]string{
	{"Zéro, le pousseur", "Le zéro ne vaut rien tout seul, mais il pousse les autres chiffres vers la gauche. Sans lui, 5 et 50 seraient jumeaux.", "Zero the pusher", "Zero is worth nothing on its own, but it pushes the other digits to the left. Without it, 5 and 50 would be twins."},
	{"Le truc des neuf", "Pour multiplier par 9 avec tes doigts, plie le doigt du chiffre. Les doigts à gauche sont les dizaines, ceux à droite les unités.", "The nine trick", "To multiply by 9 with your fingers, fold the finger of the number. Fingers on the left are the tens, fingers on the right the ones."},
	{"Pair ou impair", "Un nombre est pair quand on peut le partager en deux sans reste. Les impairs, eux, gardent toujours un petit solitaire.", "Even or odd", "A number is even when you can share it in two with nothing left. Odd ones always keep one little loner."},
	{"Le double secret", "Double un nombre deux fois et tu obtiens toujours un multiple de quatre. Essaie avec 6 : cela donne 12, puis 24.", "The secret double", "Double a number twice and you always get a multiple of four. Try it with 6: that gives 12, then 24."},
	{"La douzaine", "Une douzaine, c’est 12. On peut la couper en 2, 3, 4 ou 6 parts égales sans rien jeter. Pratique pour partager des œufs.", "The dozen", "A dozen is 12. You can cut it into 2, 3, 4 or 6 equal parts without wasting anything. Handy for sharing eggs."},
	{"Deux tables pour une", "7 × 3 et 3 × 7 donnent le même résultat. Quand tu apprends une table, tu en apprends déjà la moitié d’une autre.", "Two tables for one", "7 × 3 and 3 × 7 give the same result. When you learn one table, you already learn half of another."},
	{"Deux impairs", "Quand tu additionnes deux nombres impairs, le résultat est toujours pair. Essaie : 7 + 9 fait 16, et 3 + 5 fait 8.", "Two odds", "When you add two odd numbers, the result is always even. Try it: 7 + 9 makes 16, and 3 + 5 makes 8."},
	{"Multiplier par dix", "Multiplier par 10 ne colle pas un zéro : ça décale chaque chiffre d’un rang vers la gauche. Le zéro vient juste combler la place.", "Times ten", "Multiplying by 10 does not stick on a zero: it shifts every digit one place to the left. The zero just fills the gap."},
	{"Les multiples de cinq", "Les multiples de 5 finissent toujours par 0 ou par 5. Regarde le dernier chiffre et tu sais déjà si 5 peut entrer.", "Multiples of five", "Multiples of 5 always end in 0 or 5. Look at the last digit and you already know whether 5 fits."},
	{"Un million", "Un million, c’est mille fois mille. Si tu comptais un nombre par seconde, il te faudrait environ douze jours sans dormir.", "One million", "A million is a thousand times a thousand. If you counted one number per second, it would take about twelve days without sleeping."},
	{"Le tour de Gauss", "Pour additionner de 1 à 10, associe 1 et 10, puis 2 et 9. Chaque paire fait 11, et il y en a 5. Donc 55 !", "The Gauss trick", "To add from 1 to 10, pair 1 with 10, then 2 with 9. Every pair makes 11, and there are 5 of them. So 55!"},
	{"Les palindromes", "Certains nombres se lisent pareil dans les deux sens, comme 121 ou 3443. On les appelle des palindromes.", "Palindromes", "Some numbers read the same both ways, like 121 or 3443. They are called palindromes."},
	{"Les multiples de trois", "Pour savoir si un nombre se partage en 3, additionne ses chiffres. Pour 123, 1 + 2 + 3 fait 6, qui se partage en 3. Oui !", "Multiples of three", "To know if a number splits into 3, add its digits. For 123, 1 + 2 + 3 makes 6, and 6 splits into 3. So yes!"},
	{"Les nombres carrés", "1, 4, 9, 16, 25 : ce sont des nombres carrés. On peut les dessiner avec des points rangés en un carré parfait.", "Square numbers", "1, 4, 9, 16, 25: these are square numbers. You can draw them with dots arranged in a perfect square."},
	{"La moitié de la moitié", "La moitié de la moitié, c’est un quart. Et la moitié d’un quart, un huitième. Une pizza se coupe vite avec ça.", "Half of a half", "Half of a half is a quarter. And half of a quarter is an eighth. A pizza gets cut quickly with that."},
	{"La preuve par l’addition", "Pour vérifier 52 − 17 = 35, additionne 35 et 17 : tu retombes sur 52. Les soustractions adorent qu’on les vérifie.", "Proof by addition", "To check 52 − 17 = 35, add 35 and 17: you land back on 52. Subtractions love being double-checked."},
}

// feedChallenges holds the challenge seeds: title fr, text fr, title en, text en.
var feedChallenges = [][4]string{
	{"Trois d’affilée", "Trois bonnes réponses d’affilée {iW} ? Je te parie que tu peux.", "Three in a row", "Three right answers in a row {iW}? I bet you can."},
	{"Ta table préférée", "Joue un chapitre {dW} en visant ta table préférée.", "Your favorite table", "Play a chapter {dW} and aim for your favorite times table."},
	{"Explique-le", "Explique ta dernière réponse à quelqu’un de chez toi, puis file {iW}.", "Explain it", "Explain your last answer to someone at home, then head {iW}."},
	{"Avant le goûter", "Un petit chapitre {dW} avant le goûter, ça te dit ?", "Before snack time", "A little chapter {dW} before snack time, what do you say?"},
	{"Devine d’abord", "Invente un calcul tout seul, devine le résultat, puis vérifie {iW}.", "Guess first", "Make up a calculation yourself, guess the result, then check {iW}."},
	{"De cinq en cinq", "Compte de cinq en cinq jusqu’à cinquante, puis ouvre {W}.", "Count by fives", "Count by fives up to fifty, then open {W}."},
	{"Tout en douceur", "Fais un chapitre {dW} en le prenant le plus calmement possible.", "Nice and easy", "Do a chapter {dW} as calmly as you possibly can."},
	{"Chuchote-le", "Chuchote ta réponse avant de la toucher, puis joue {iW}.", "Whisper it", "Whisper your answer before tapping it, then play {iW}."},
	{"Le pair caché", "Trouve un nombre pair caché dans ta journée, puis passe {iW}.", "The hidden even", "Find an even number hidden in your day, then drop by {iW}."},
	{"Un monde oublié", "Un monde que tu n’as pas visité depuis longtemps t’attend : {W}.", "A forgotten world", "A world you have not visited in a long time is waiting: {W}."},
	{"La chasse aux cartes", "Une carte est à gagner {iW}. Prêt pour la chasse ?", "The card hunt", "A card is up for grabs {iW}. Ready for the hunt?"},
	{"Le duel familial", "Défie quelqu’un de ta famille avec un chapitre {dW}, puis comparez vos histoires.", "The family duel", "Challenge someone in your family with a chapter {dW}, then compare your stories."},
}

// feedPuzzles holds the puzzle intro sentences: fr, en.
var feedPuzzles = [][2]string{
	{"Une porte {dW} reste fermée tant que personne ne trouve le bon produit.", "A door {dW} stays shut until somebody finds the right product."},
	{"{iW}, un coffre demande une multiplication avant de s’ouvrir.", "{iW}, a chest asks for a multiplication before it opens."},
	{"Un gardien {dW} pose sa question préférée sur les tables.", "A guardian {dW} asks his favorite question about the times tables."},
	{"Le pont {dW} se baisse seulement pour qui sait calculer ça.", "The bridge {dW} lowers only for whoever can work this out."},
	{"Un petit robot {dW} a perdu le résultat de sa multiplication.", "A little robot {dW} lost the result of its multiplication."},
	{"Les lanternes {dW} s’allument une à une si tu trouves le bon nombre.", "The lanterns {dW} light up one by one if you find the right number."},
	{"Un message secret {dW} cache un produit à retrouver.", "A secret message {dW} hides a product to find."},
	{"Le héros {dW} reste coincé devant une grille de nombres.", "The hero {dW} is stuck in front of a grid of numbers."},
	{"Un coffre de pirate {dW} réclame une multiplication bien faite.", "A pirate chest {dW} demands a well-made multiplication."},
	{"La clé {dW} ne tourne que pour le bon produit.", "The key {dW} turns only for the right product."},
}

// feedPipOpen holds Numa openers: fr, en.
var feedPipOpen = [][2]string{
	{"Psst, {n}, j’ai une idée.", "Psst, {n}, I have an idea."},
	{"Prêt, {n} ? Numa a du neuf.", "Ready, {n}? Numa has news."},
	{"Regarde ce que j’ai trouvé, {n}.", "Look what I found, {n}."},
	{"{n}, installe-toi, Numa raconte.", "Settle in {n}, Numa is talking."},
	{"Coucou {n} ! Devine un peu.", "Hi {n}! Guess what."},
	{"Eh {n}, une minute pour moi ?", "Hey {n}, got a minute?"},
	{"Alors {n}, une petite surprise ?", "So {n}, a little surprise?"},
	{"Dis {n}, tu aimes les mystères ?", "Tell me {n}, do you like mysteries?"},
}

// feedPipClose holds Numa closers: fr, en.
var feedPipClose = [][2]string{
	{"Tu en penses quoi, {n} ?", "What do you think, {n}?"},
	{"Moi, ça me fait sourire.", "That makes me smile."},
	{"On en reparle bientôt.", "We will talk again soon."},
	{"Allez, {n}, je file !", "Right, {n}, off I go!"},
	{"Devineras-tu la suite ?", "Can you guess what is next?"},
	{"Numa a parlé !", "Numa has spoken!"},
	{"À toi de jouer, {n}.", "Your turn, {n}."},
	{"Raconte-moi ce que tu trouves.", "Tell me what you find."},
}

// feedLumiOpen holds Numa openers: fr, en.
var feedLumiOpen = [][2]string{
	{"Hou hou, {n} ! Un casse-tête.", "Hoo hoo, {n}! A puzzle."},
	{"{n}, j’ai caché une énigme.", "{n}, I hid a riddle."},
	{"Mes plumes frissonnent, {n} : défi !", "My feathers tingle, {n}: a challenge!"},
	{"Alors {n}, les yeux grands ouverts ?", "So {n}, eyes wide open?"},
	{"Pause réflexion, {n}.", "Thinking break, {n}."},
	{"Chut {n}, écoute cette énigme.", "Shh {n}, listen to this riddle."},
	{"Numa te lance un défi, {n}.", "Numa throws you a challenge, {n}."},
}

// feedLumiClose holds Numa closers: fr, en.
var feedLumiClose = [][2]string{
	{"Prends ton temps, la nuit est longue.", "Take your time, the night is long."},
	{"Qu’en dis-tu, {n} ?", "What do you say, {n}?"},
	{"Choisis bien, les chouettes se trompent rarement.", "Choose well, owls are rarely wrong."},
	{"Un indice : respire, puis compte.", "A hint: breathe, then count."},
	{"Je sais que tu peux, {n}.", "I know you can, {n}."},
	{"Hou ! J’attends ta réponse.", "Hoo! I am waiting for your answer."},
}
