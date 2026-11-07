package tasks

// Hand-written threads for craft and industry topics; IDs and versions are assigned in warmupConversations.
var warmupLibraryCraft = []Conversation{
	{
		Theme:       "writing",
		Subject:     "Shorter emails, faster replies",
		Description: "Lately I've been trying to keep my work emails to five sentences or fewer, and something odd happened. People reply faster, and their replies are clearer too. Have you ever put a hard limit on yourself like that?",
		Messages: []string{
			"I haven't set a strict number, but I do put the actual ask in the first line now. I used to bury it under three paragraphs of context and then wonder why nobody answered. Do you find the limit forces you to drop anything important?",
			"Sometimes, but usually what gets dropped is the throat-clearing at the start. If someone needs the background, they ask, and that happens far less often than I expected.",
			"Throat-clearing is exactly the right word for it. I'm going to try your limit for the rest of the month and see how it goes.\n\nCheers,",
		},
	},
	{
		Theme:       "support",
		Subject:     "Tone in support replies",
		Description: "Hi,\n\nQuick question for you, since you've run a help desk before. When a customer writes in clearly annoyed, do you match their energy a little or stay completely even? I keep going back and forth on it.",
		Messages: []string{
			"Stay even, but not flat. I acknowledge the frustration in one plain sentence, then move straight to what I can actually do. Matching their energy tends to escalate things, and sounding stiff makes it worse.",
			"That makes sense. I think my real problem is the apology. I either skip it entirely or write three of them in a row.",
			"One is plenty, and it lands better when it names the specific thing that went wrong rather than a general sorry. You'll find your rhythm with it quickly.",
		},
	},
	{
		Theme:       "design",
		Subject:     "Homepage layout question",
		Description: "I'm reworking the layout of my portfolio site and I'm stuck on the homepage. Right now it has a big intro block, then a grid of projects, then a short about section. It feels crowded, though, and I can't tell which part to cut.\n\nWhat would you look at first if it were yours?",
		Messages: []string{
			"I'd shrink the intro block before cutting anything. Most visitors want to see work quickly, so if the grid shows up without scrolling, the page already feels calmer. The about section can stay small at the end.",
			"Good point. The intro takes up half the screen at the moment. I'll trim it to a single line and a short paragraph.",
			"That should open things up a lot. Sounds like a good plan to me.\n\nAll the best,",
		},
	},
	{
		Theme:       "typography",
		Subject:     "Picking a body font",
		Description: "Do you have a rule of thumb for choosing a body font? I've narrowed it down to a serif and a sans serif, and I like them about equally.",
		Messages: []string{
			"Set a long paragraph in each at the size people will actually read it, then step away for a few minutes. Whichever one you stop noticing is usually the right choice for body text.",
			"That's a nice way to decide. The serif looked lovely as a heading, but in a full paragraph it got a bit busy.",
			"Keep it for headings then, and use the sans serif for everything else. A little contrast between the two usually works well.",
		},
	},
	{
		Theme:       "spreadsheets",
		Subject:     "A spreadsheet habit worth sharing",
		Description: "Hey there,\n\nSmall thing, but I finally started locking the header row in place on every spreadsheet I make, and I can't believe how long I scrolled without it. Now I'm wondering what other basic habits I've been missing.",
		Messages: []string{
			"Named ranges changed things for me. Instead of a formula full of cell references, you read something like total hours divided by weeks, and six months later you still know what it does.",
			"I've never used those. Do they hold up when you insert new rows in the middle?",
			"They do, as long as the rows go inside the range rather than right after the last one. Worth a try on your next sheet.\n\nTalk soon,",
		},
	},
	{
		Theme:       "retail",
		Subject:     "Keeping the back room tidy",
		Description: "The back room of the shop gets out of hand every few weeks, and I end up spending a whole afternoon sorting it. I've tried bins, labels and a whiteboard. None of it sticks for long. How do you keep your stockroom in order?",
		Messages: []string{
			"Honestly, the thing that worked was a rule that nothing goes on the floor, ever. If a box comes in and there's no shelf space, something on the shelf has to go first. It forces small decisions every day instead of one big sort.",
			"Nothing on the floor. That's simple enough that I might actually stick to it, thank you.",
		},
	},
	{
		Theme:       "clients",
		Subject:     "A client who keeps changing direction",
		Description: "Good morning,\n\nI could use a second opinion. I have a client who changes the brief after every round of drafts, and each change is reasonable on its own. Together they've turned a small project into something much bigger. I'd like to raise it without sounding irritated, but I'm not sure how to start that conversation.",
		Messages: []string{
			"I'd keep it factual and forward-looking. Something like: the project has grown since we started, so let's agree on what the final version needs to include before the next round. It names the problem without blaming anyone.",
			"That's much calmer than what I had drafted. Should I mention how many rounds we've already been through?",
			"Only if they push back. Leading with the count can feel like keeping score, and you want them on your side for the rest of the project.\n\nBest of luck,",
		},
	},
	{
		Theme:       "pricing",
		Subject:     "How you price project work",
		Description: "I'm rethinking how I price projects. Charging by the hour has started to feel like it punishes me for getting faster. Did you ever switch to pricing by the project, and was it worth the trouble?",
		Messages: []string{
			"I did, a few years ago. The hard part was figuring out how to scope carefully, because a vague brief becomes your problem instead of the client's. Once I got better at writing down exactly what was included, it felt much fairer to everyone.",
			"So the scope document does most of the work. I've been treating mine like a formality.",
			"It really is the heart of it. A clear list of what's in and what's out saves a lot of awkward conversations later.",
		},
	},
	{
		Theme:       "routines",
		Subject:     "Structure for solo workdays",
		Description: "Working for myself has been great, but my days have no shape at all. Some mornings I start at dawn, some I don't start until after lunch, and by the end of the week I can't tell where the hours went.\n\nYou've done this longer than I have. Did you settle into a routine, or does it stay loose?",
		Messages: []string{
			"It took me a while, but I ended up with a fixed start and a fixed stop, and everything in between is flexible. The stop matters more than the start. Without it, the work just leaks into the evening.",
			"A fixed stop is a good idea. I think I've been trying to control the start instead.",
			"Most people do. Give the stop a couple of weeks and see how it feels.\n\nTake care,",
		},
	},
	{
		Theme:       "newsletter",
		Subject:     "Your short pieces lately",
		Description: "Your newsletter has become one of the few things I read start to finish. The short pieces about small decisions in your work are my favourite part.",
		Messages: []string{
			"That's so kind of you to say. Those are the ones I enjoy writing most, though I always worry they're too small to be worth sending.",
			"They're the opposite of too small. They feel like a conversation instead of an announcement, which is rare.",
			"That's lovely to hear, and it helps on the weeks I'm second-guessing an issue. Thank you for writing.\n\nAll my best,",
		},
	},
	{
		Theme:       "community",
		Subject:     "Staying after the meetup talks",
		Description: "Hi,\n\nI've been to the local design meetup twice now and I keep leaving early. The talks are good, but the mingling afterward feels like a lot. Do you usually stay for that part?",
		Messages: []string{
			"I used to skip it too. What helped was going in with one small goal, like asking a single person what they're working on. Once that's done, I can leave without feeling like I missed anything.",
			"One person feels manageable. I think I've been treating it like I need to meet the whole room.",
			"Nobody manages the whole room. One good conversation is a successful evening.",
			"Fair enough, that takes a lot of the pressure off. Thanks for the nudge.",
		},
	},
	{
		Theme:       "workshop",
		Subject:     "Crooked notebooks and a sore thumb",
		Description: "Finally made it to a bookbinding workshop after meaning to go for ages. I came home with three slightly crooked notebooks and a very sore thumb, and I loved every minute of it.",
		Messages: []string{
			"Crooked notebooks are the best kind. What did they have you start with?",
			"A simple pamphlet stitch, then a longer binding with several sections sewn together. The second one is where my thumb gave up, but it's also the one I'm proudest of.",
			"Sounds like a wonderful way to spend a day. Enjoy filling those notebooks.",
		},
	},
	{
		Theme:       "social",
		Subject:     "Stepping back from posting",
		Description: "Posting has been on pause for me for a few weeks, and the quiet has been strange but nice. I was spending more time thinking about what to share than actually making things. I'm not sure yet whether I'll go back.",
		Messages: []string{
			"I took a similar break a while ago and noticed the same thing. The work got better almost right away, mostly because I stopped shaping it around what would get attention.",
			"That's what worries me a little, though. Do you think stepping away cost you anything with the people who follow your work?",
			"A bit of reach, maybe, but the people who cared were still there when I came back. I'd enjoy the quiet while it lasts.",
		},
	},
	{
		Theme:       "feedback",
		Subject:     "Feedback on the order form",
		Description: "We put the reworked order form in front of a handful of regular customers, and the feedback surprised me. Nobody mentioned the colours or the new layout we spent ages on. Almost every comment was about one confusing label on the delivery field.\n\nIt's humbling how often the small stuff matters most.",
		Messages: []string{
			"That's so often how it goes. People notice whatever stops them, not whatever we're proud of. Did you change the label?",
			"Yes, to plain wording that says exactly what goes in the box. A few people have already said the form feels easier, even though nothing else changed.",
			"That's a great result for one small edit. Nice work listening so closely.",
		},
	},
	{
		Theme:       "shortcuts",
		Subject:     "A shortcut I use constantly",
		Description: "Sharing this because it saved me a surprising amount of time this week. There's a shortcut that pastes text without any of the original formatting, and I now use it dozens of times a day. Do you have a favourite one like that?",
		Messages: []string{
			"Mine is the one that reopens the last tab you closed. I close the wrong tab constantly, so it gets more use than I'd like to admit.",
			"That one would save me a lot of grief. I always end up digging through my history, so thanks for sharing it.",
		},
	},
	{
		Theme:       "backups",
		Subject:     "Finally sorted my backups",
		Description: "Hey there,\n\nI finally sorted out backups for my work files after a scare with an old laptop. Everything now copies to an external drive each night, plus a second copy kept off site. It took one afternoon and I should have done it years ago.",
		Messages: []string{
			"Good for you. The off-site copy is the part most people skip. Have you tried restoring anything yet? That's the step that catches people out.",
			"Not yet, and now that you mention it, that seems important. I'll pick a folder and try bringing it back.",
			"That's the right move. A backup you've restored once is one you can trust.",
			"Agreed. Glad you mentioned it before I found out the hard way.\n\nCheers,",
		},
	},
	{
		Theme:       "naming",
		Subject:     "Naming project folders",
		Description: "How do you name your project folders? Mine are a mess of things like final, final two and final really, and I'm embarrassed every time I share my screen.",
		Messages: []string{
			"Ha, I know that folder well. I switched to starting every name with the year and month, then the client, then a short description. They sort themselves in order and I never need the word final.",
			"Putting the date first is clever. I always started with the client name.",
			"Client first works if you mostly look things up by client. The main thing is picking one pattern and sticking with it.\n\nBest,",
		},
	},
	{
		Theme:       "sketching",
		Subject:     "Sketching before the screen",
		Description: "Paper is back in my process. I sketch ideas by hand before opening anything on the computer now, and although it feels slower, I throw away bad ideas much faster when they only cost a pencil line.\n\nCurious whether you do anything similar.",
		Messages: []string{
			"All the time. I keep a plain notebook by the desk just for rough layouts. The rule is that nothing in it has to look good, which makes it easy to fill pages.",
			"Nothing has to look good. I like that a lot, because my sketches start getting precious after the second page.",
			"Rough is the point. Good luck keeping them messy.",
		},
	},
	{
		Theme:       "photography",
		Subject:     "Photographing pieces for my portfolio",
		Description: "My ceramics photos keep coming out flat, and I'm reshooting everything for the portfolio. I'm using a window for light and a plain backdrop. Any thoughts on what I might be missing?",
		Messages: []string{
			"Try a white card on the side opposite the window. It bounces a little light back into the shadows and gives the pieces more shape without making them look lit. Shooting a bit lower than eye level helps too.",
			"I tried the card this morning and what a difference. The glaze actually looks like itself now.",
			"That's wonderful to hear! Ceramics are hard to photograph well, so that's a real win.",
		},
	},
	{
		Theme:       "bakery",
		Subject:     "End of a busy season",
		Description: "Good morning,\n\nWe've just come through the busiest season the bakery has had since we opened. It was exhausting and wonderful in equal measure, and I'm only now catching up on sleep.",
		Messages: []string{
			"That's wonderful news, and well earned. What made the difference this year, do you think?",
			"Mostly word of mouth. We kept the menu small and focused on doing a few things very well, and people seemed to tell their friends.",
			"A small menu done well is hard to beat. Enjoy a slower stretch now that it's here.\n\nAll the best,",
		},
	},
	{
		Theme:       "speaking",
		Subject:     "Talking to a class of students",
		Description: "A teacher I know asked me to give a short talk to her design students about what working life is actually like. I'm happy to do it, but I have no idea how to pitch it. Too practical and it's dull, too inspirational and it's hollow.\n\nYou've done a few of these. Where did you land?",
		Messages: []string{
			"I tell one honest story about a project that went sideways and what I learned from it. Students remember stories far longer than advice, and a bit of failure makes everything else you say more believable.",
			"That's a relief, because I have plenty of those. Any thought on how long the story should run?",
			"Keep it to a few minutes and leave plenty of room for questions. They'll take the talk somewhere useful on their own.",
		},
	},
	{
		Theme:       "shipping",
		Subject:     "Packing fragile orders",
		Description: "Do you have any tips for packing fragile items? A couple of glasses arrived chipped recently and I'd like to stop that happening.\n\nThanks,",
		Messages: []string{
			"Double boxing made the biggest difference for us. Wrap the item, put it in a small box, then float that box in a larger one with padding on every side. The inner box never touches the outer walls.",
			"Floating the inner box makes a lot of sense. I've been padding a single box and hoping for the best.",
			"Single boxes are fine for sturdy things, but glass really needs the extra layer. Hope the next batch arrives in one piece.",
		},
	},
}
