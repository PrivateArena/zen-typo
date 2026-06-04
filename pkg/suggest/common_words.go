package suggest

var commonEnglishWords = map[string]int64{
	// Pronouns & Basic Determiners
	"i": 50000, "you": 45000, "he": 35000, "she": 30000, "it": 48000, "we": 25000, "they": 28000,
	"me": 18000, "him": 15000, "her": 22000, "us": 12000, "them": 16000,
	"my": 32000, "your": 25000, "his": 22000, "its": 18000, "our": 15000, "their": 16000,
	"this": 38000, "that": 45000, "these": 15000, "those": 10000,

	// Articles, Conjunctions & Prepositions
	"the": 150000, "a": 100000, "an": 35000, "and": 90000, "but": 30000, "or": 25000, "if": 20000,
	"because": 12000, "as": 28000, "of": 95000, "to": 90000, "in": 80000, "for": 50000, "on": 45000,
	"with": 40000, "at": 35000, "by": 30000, "from": 28000, "about": 22000, "into": 15000,
	"through": 10000, "over": 12000, "after": 11000, "before": 9000, "between": 8000,
	"under": 7000, "during": 6000, "without": 8000, "against": 5000,

	// Auxiliary & Common Verbs
	"be": 80000, "am": 15000, "is": 75000, "are": 60000, "was": 55000, "were": 45000, "been": 25000,
	"have": 50000, "has": 28000, "had": 24000, "do": 40000, "does": 14000, "did": 22000, "done": 12000,
	"will": 35000, "would": 25000, "should": 15000, "could": 18000, "can": 38000, "cannot": 8000,
	"may": 12000, "might": 9000, "must": 10000, "make": 18000, "made": 14000, "go": 22000,
	"went": 12000, "gone": 8000, "get": 24000, "got": 16000, "know": 20000, "knew": 7000,
	"take": 15000, "took": 9000, "see": 16000, "saw": 8000, "seen": 9000, "come": 15000,
	"came": 10000, "think": 22000, "thought": 14000, "look": 16000, "looked": 10000, "want": 18000,
	"wanted": 11000, "give": 12000, "gave": 6000, "use": 14000, "used": 15000, "find": 11000,
	"found": 9000, "tell": 10000, "told": 9000, "ask": 9500, "asked": 8500,
	"worked": 8000, "feel": 10500, "felt": 8000, "try": 9000, "tried": 7500, "leave": 9500,
	"left": 11000, "call": 9000, "called": 9500, "say": 25000, "said": 30000, "mean": 8000,
	"keep": 7500, "kept": 5000, "seem": 8500, "seemed": 7000, "help": 9000, "show": 8000,
	"play": 7000, "run": 8000, "write": 9000, "wrote": 5000, "read": 9500, "live": 8500,

	// Adjectives & Adverbs
	"good": 25000, "great": 15000, "well": 22000, "best": 12000, "better": 11000, "bad": 8000,
	"worse": 4000, "new": 28000, "old": 18000, "young": 8000, "first": 20000, "second": 10000,
	"last": 14000, "next": 12000, "early": 8000, "late": 7500, "high": 12000, "low": 9000,
	"big": 11000, "small": 10500, "large": 9500, "long": 13000, "short": 8500, "hot": 6000,
	"cold": 6500, "warm": 5500, "dark": 6000, "light": 9000, "easy": 8000, "hard": 9000,
	"clear": 7500, "full": 7000, "empty": 4500, "different": 11000, "same": 14000, "important": 12000,
	"very": 25000, "too": 18000, "also": 22000, "only": 20000, "just": 28000, "even": 18000,
	"almost": 9000, "always": 12000, "never": 14000, "sometimes": 8500, "often": 9500, "usually": 7000,
	"here": 18000, "there": 28000, "again": 12000, "still": 13000, "yet": 9000, "now": 22000,
	"then": 24000, "once": 8000, "twice": 4000, "soon": 9500, "today": 12000, "tomorrow": 9000,
	"yesterday": 8500, "maybe": 9500, "perhaps": 8000,

	// Key Nouns (including "door" and related vocabulary)
	"door": 8000, "doors": 4500, "window": 7500, "windows": 4000, "house": 15000, "home": 22000,
	"room": 11000, "rooms": 5000, "floor": 8000, "wall": 6000, "roof": 4000, "bed": 7000,
	"table": 8000, "chair": 6000, "street": 7500, "road": 8000, "city": 11000, "town": 7500,
	"country": 13000, "world": 18000, "earth": 7000, "sky": 6000, "sun": 7500, "moon": 5500,
	"water": 16000, "food": 14000, "meat": 5000, "bread": 4500, "milk": 5500, "book": 12000,
	"paper": 9000, "pen": 4000, "letter": 7500, "word": 12000, "words": 14000, "name": 14000,
	"number": 13000, "time": 35000, "year": 22000, "years": 25000, "day": 24000, "days": 20000,
	"week": 12000, "month": 11000, "hour": 9000, "minute": 8000,
	"people": 28000, "man": 18000, "woman": 16000, "child": 12000, "children": 14000,
	"father": 11000, "mother": 12000, "brother": 8000, "sister": 8000, "friend": 16000,
	"friends": 14000, "family": 15000, "car": 12000, "bus": 5000, "train": 6500,
	"plane": 5500, "boat": 5000, "ship": 6000, "animal": 7500, "dog": 11000,
	"cat": 9000, "bird": 7000, "fish": 7500, "tree": 8000, "flower": 6000,
	"money": 13000, "job": 11000, "work": 22000, "school": 14000, "class": 8500,
	"office": 9000, "store": 8000, "shop": 7500, "bag": 5500, "box": 7000,
	"key": 6500, "game": 9000, "music": 11000, "song": 7000, "picture": 9000,
	"body": 9500, "head": 11000, "face": 10000, "eye": 12000, "eyes": 16000,
	"ear": 6000, "nose": 5000, "mouth": 7500, "hair": 7000, "hand": 13000,
	"hands": 12000, "arm": 6000, "leg": 6500, "foot": 7000, "feet": 8000,
	"heart": 9500, "blood": 7000, "voice": 8500, "sound": 8500,

	// Technology & Custom domain words
	"computer": 8500, "system": 12000, "systems": 9000, "program": 8000, "software": 7500,
	"file": 8000, "files": 7000, "data": 12000, "information": 13000, "code": 9500,
	"internet": 9000, "network": 8000, "user": 9000, "users": 7500, "screen": 8000,
	"keyboard": 6000, "mouse": 5500, "phone": 9500, "device": 8000, "devices": 7000,
	"linux": 6000, "hello": 15000, "typo": 8000, "typography": 5000, "predict": 6000,
	"suggest": 7000, "suggestion": 7500, "complete": 8000, "learning": 8500,
}
