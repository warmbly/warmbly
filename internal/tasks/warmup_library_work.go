package tasks

// Hand-written threads for work topics; IDs and versions are assigned in warmupConversations.
var warmupLibraryWork = []Conversation{
	{
		Theme:       "planning",
		Subject:     "Quarter planning, round two",
		Description: "I'm trying to plan next quarter without overcommitting the way I did last time. How do you decide how much goes on the list before it turns into a wish list?",
		Messages: []string{
			"I pick three things that would make the quarter a success if nothing else happened, then add maybe two smaller items. Anything past that goes on a parking list I look at halfway through.",
			"A parking list is a good idea. Right now everything sits in one long column and it all looks equally important. Do you share the parking list with the team or keep it to yourself?",
			"I share it, mostly so nobody thinks their idea vanished. It also stops the same request from coming back every few weeks. Sounds like you're already on the right track with the plan.",
		},
	},
	{
		Theme:       "meetings",
		Subject:     "Shorter meetings",
		Description: "Hi,\n\nWe cut our standing team meeting from an hour to thirty minutes and, oddly, we get more done in it. I think having less time makes people come with a point instead of thinking out loud. Have you tried anything similar?",
		Messages: []string{
			"We did the same with our planning meeting. The trick for us was a short written agenda the day before, so the meeting itself is for decisions, not updates.",
			"That makes sense. Updates were eating most of our time too, so moving them into writing seems like the obvious next step.",
			"Hope the team takes to it. Ours grumbled for a week and then nobody wanted to go back.\n\nCheers,",
		},
	},
	{
		Theme:       "onboarding",
		Subject:     "Ideas for a new hire's first week",
		Description: "We have someone joining the team soon and I'd like their first week to feel less like drinking from a fire hose. What did you find most useful when you started your current role?",
		Messages: []string{
			"Honestly, a short list of people to meet and why. Knowing who to ask about what saved me weeks. A small first task they can finish in a few days helps too, so they get an early win.",
			"The people list is a great idea. I was planning to hand over a pile of documents and hope for the best.",
			"Documents are fine as a reference, just not as the whole plan. Sounds like they're joining a thoughtful team.",
		},
	},
	{
		Theme:       "documentation",
		Subject:     "Writing docs people read",
		Description: "Morning,\n\nI've been asked to tidy up our internal docs, and the first thing I noticed is that half of them answer questions nobody asks anymore.\n\nDo you have a way of deciding what to keep and what to archive? I'm tempted to start from scratch, but that feels like a trap.",
		Messages: []string{
			"Starting from scratch usually is a trap. I'd look at which pages people actually open and ask the team what they searched for and couldn't find. Archive the rest instead of deleting it, so nobody panics.",
			"Archiving rather than deleting will save a few arguments. Is there a length you aim for per page?",
			"Short enough to read in one sitting. If a page needs its own table of contents, it probably wants to be two pages.",
			"That's a useful rule of thumb. Thanks for the sanity check.",
		},
	},
	{
		Theme:       "feedback",
		Subject:     "A question about giving feedback",
		Description: "I need to give some critical feedback to a colleague who is generally great but keeps missing details in handoffs. I don't want it to sound bigger than it is. Any advice on keeping it light without being vague?",
		Messages: []string{
			"Be specific about one or two recent moments and what the effect was, then ask how they see it. Keeping it short and private does most of the work of keeping it light.",
			"Asking how they see it is the part I would have skipped. That helps.\n\nThanks,",
		},
	},
	{
		Theme:       "remote",
		Subject:     "Desk setup upgrade",
		Description: "After months on a kitchen chair for remote days, I finally replaced it with a proper one. My back noticed within a week. Did you change anything about your setup that turned out to matter more than you expected?",
		Messages: []string{
			"A second monitor at the right height, oddly enough. I'd been looking down at a laptop for years without realising how much it was straining my neck.",
			"Monitor height is next on my list then. I've been propping the laptop on a stack of books.",
			"Books work better than nothing. Enjoy the new chair.",
		},
	},
	{
		Theme:       "notes",
		Subject:     "How do you take meeting notes",
		Description: "Hey,\n\nQuick one. I keep losing track of what was agreed in meetings because my notes end up as a wall of half sentences. How do you take yours?",
		Messages: []string{
			"I keep three headings for every meeting: decisions, actions, open questions. Everything else is optional. If I only fill in those three, the notes are still useful a month later.",
			"That's so much simpler than what I do. Do you write them during the meeting or right after?",
			"During, but I spend a couple of minutes cleaning them up right after while it's fresh. Hope the headings help.",
		},
	},
	{
		Theme:       "handoff",
		Subject:     "Handing over the project",
		Description: "I'm moving to a different team soon and handing my current project to someone who hasn't worked on it. I've written up the obvious parts, but I'm worried about the things I know without realising I know them. How would you go about catching those?",
		Messages: []string{
			"Have them shadow you for a few days and write down every question they ask. Those questions are exactly the knowledge that never made it into the write-up. Then add the answers to the doc.",
			"Letting their questions shape the document is clever. It turns the gaps into a list instead of guesswork.",
			"That's the idea. Good luck with the new team.",
		},
	},
	{
		Theme:       "priorities",
		Subject:     "Too many priorities",
		Description: "Everything on my plate is labelled high priority right now, which means nothing really is. I've started ranking tasks by what breaks if I don't do it this week. Curious whether you use something smarter.",
		Messages: []string{
			"That's close to what I do. I also ask who is waiting on each item. Work that holds up other people goes first, even if it feels less important to me.",
			"Holding up others is a good tiebreaker. I tend to forget that part because it isn't visible on my own list.",
			"It took me a while to notice it too. Your ranking sounds solid already.\n\nBest,",
		},
	},
	{
		Theme:       "hiring",
		Subject:     "Interview questions that work",
		Description: "Hi,\n\nI'm on an interview panel for the first time in a while, and the standard question list feels stiff. Do you have a question that reliably tells you something useful about a candidate?",
		Messages: []string{
			"I ask them to walk me through a decision they'd make differently now. The answer shows how they reflect and whether they can talk about a mistake without getting defensive.",
			"I like that. It's open without being a trick question, and it gives people room to be honest. Thanks for passing it on.",
		},
	},
	{
		Theme:       "presentations",
		Subject:     "Slides or no slides",
		Description: "I have to present our team's results to a larger group and I'm going back and forth on whether to use slides at all. Last time the slides took over and nobody remembered the main point.",
		Messages: []string{
			"Maybe a handful of slides with one sentence each, and the main point stated in the first minute and again at the end. People remember what they hear twice.",
			"Saying it twice feels repetitive when I'm the one talking, but you're right that it isn't for me. A handful of slides it is.",
			"Exactly. It always feels like too much from the front of the room. You'll do well.",
		},
	},
	{
		Theme:       "email",
		Subject:     "Taming the inbox",
		Description: "My inbox has gotten out of hand. I spend the first hour of every day just sorting. I've tried folders and rules, but they mostly hide things from me. What has worked for you?\n\nThanks,",
		Messages: []string{
			"I stopped filing altogether. I archive everything once it's handled and rely on search to find it again. I also check mail at set points in the day instead of keeping it open.",
			"Checking at set points is the hard part for me. I always feel like I'll miss something important.",
			"That feeling fades. Anything truly pressing tends to arrive by a quicker route anyway.",
			"Fair point. This is reassuring, thanks.",
		},
	},
	{
		Theme:       "rituals",
		Subject:     "Team rituals worth keeping",
		Description: "We're thinking about which team rituals to keep now that the team has grown. The weekly show-and-tell is popular, but the daily check-in feels like it's running on habit. Have any of yours lasted over the years?",
		Messages: []string{
			"Our monthly lunch where nobody talks about work has outlasted everything else. The daily check-in moved to writing a while ago and nobody misses the call.",
			"A lunch with no work talk sounds lovely. We might borrow that one.",
			"Please do. It's the easiest one to keep going.",
		},
	},
	{
		Theme:       "skills",
		Subject:     "Getting better at writing queries",
		Description: "I've decided to learn enough SQL to stop waiting on others for simple reports. I'm a few weeks in and the basics make sense, but joins still feel like guesswork. Did anything make it fall into place for you?",
		Messages: []string{
			"Drawing the tables on paper and tracing which rows match. It sounds silly, but seeing it laid out made joins much less mysterious. Practising on data you already know helps a lot too.",
			"Data I already know is a good tip. The practice sets I've been using are about imaginary shops, so I never notice when a result looks wrong.",
			"That's the problem with practice data. Your own reports will teach you faster. Enjoy it.",
		},
	},
	{
		Theme:       "talks",
		Subject:     "Thinking about giving a talk",
		Description: "Something I've been weighing: submitting a talk to an industry conference for the first time. I have a topic I care about, but I'm nervous it's too niche. How did you decide on your first one?",
		Messages: []string{
			"Niche is usually a strength. Organisers get a lot of broad overview proposals, and a talk about one problem you've solved in depth stands out. I picked mine because I'd explained it to colleagues so many times.",
			"That's encouraging. I've definitely explained my topic more than once at work, so maybe that's a sign.",
			"It's a good sign. Best of luck with the proposal.\n\nCheers,",
		},
	},
	{
		Theme:       "mentoring",
		Subject:     "Thank you for the advice",
		Description: "I wanted to say thanks for the advice you gave me when I was unsure about leading the new project. I took it on, and it has been the most useful stretch of my year so far.",
		Messages: []string{
			"That's really good to hear. You did the hard part by saying yes, and I'm glad it's going well.",
			"Hearing that feeling unready was normal made a big difference. Thanks again.",
		},
	},
	{
		Theme:       "process",
		Subject:     "Changing our review process",
		Description: "Our team is changing how work gets reviewed, moving from one reviewer to two for anything customer facing. Some people see it as extra bureaucracy. How did your team handle a process change that not everyone wanted?",
		Messages: []string{
			"We ran it as a trial for a set period and agreed to look at the results together afterwards. People were much more willing when they knew it wasn't permanent by default.",
			"A trial period is a good way to lower the stakes. Did anything change after you reviewed it?",
			"We kept the change but dropped one step that nobody found useful. Having everyone's input made the final version easier to accept.",
			"That's reassuring. Thanks for walking me through it.",
		},
	},
	{
		Theme:       "scope",
		Subject:     "Scope creep again",
		Description: "Hey,\n\nA project I'm on has started growing. Every review adds one more small feature, and the small features are adding up. I need to push back without sounding difficult. How do you usually phrase it?",
		Messages: []string{
			"I try to make it a trade rather than a refusal. Something like: happy to add that, which of these other items should move to make room. It keeps the conversation about choices rather than about me saying no.",
			"Framing it as a trade is much easier than a flat no. It also puts the decision with the person asking.",
			"That's the part that tends to slow the requests down. Good luck holding the line.",
		},
	},
	{
		Theme:       "focus",
		Subject:     "Blocking time for focused work",
		Description: "Two mornings a week are now blocked off for focused work on my calendar, with no meetings allowed. It's only been a few weeks, but I've finished more in those mornings than in the rest of the week combined.",
		Messages: []string{
			"Same experience here. The hard part is protecting the blocks when someone asks for just a quick call. I've learned to offer another slot instead of giving the morning away.",
			"Offering another slot is a good habit. I've already caved once, so that's useful to hear.",
			"Everyone caves once or twice. It gets easier after the first few times you hold on to it.",
		},
	},
	{
		Theme:       "retrospectives",
		Subject:     "Retros that lead somewhere",
		Description: "Every retrospective we run produces a good list of issues, and then nothing happens. By the next one, we're writing the same points again. I'd love to hear how you make them stick.",
		Messages: []string{
			"We limit ourselves to one change per retro and give it a single owner. The first few minutes of the next one are spent checking whether it happened. One change that sticks beats five that don't.",
			"One change at a time is a big shift from our usual long list, but it makes sense. Thanks, that's helpful.\n\nBest,",
		},
	},
	{
		Theme:       "async",
		Subject:     "Writing better updates",
		Description: "Our team spans a few time zones, so most updates happen in writing now. The problem is that long messages get skimmed and short ones lack context. Have you found a format that works?",
		Messages: []string{
			"We lead with one line saying what the reader needs to do, if anything, then the details below. People can stop after the first line if it doesn't concern them.",
			"Putting the action first is so obvious once you say it. Half our updates bury it at the end.",
			"Most do. It took us a few months of reminding each other before it became a habit.",
		},
	},
	{
		Theme:       "career",
		Subject:     "Weighing a role change",
		Description: "I've been offered a move from a specialist role into a team lead position. I like the work I do now, and I'm not sure I'd enjoy spending more time on people and less on the craft. Have you ever faced a choice like that?\n\nThanks,",
		Messages: []string{
			"I did a few years ago. What helped was asking a couple of leads what their week actually looked like, not what the job description said. Some of them still did plenty of hands-on work, and some barely touched it.",
			"Asking about an actual week is a good idea. The description I have is very vague.",
			"Then those conversations will tell you more than the title. Whatever you decide, it sounds like you're giving it the thought it deserves.",
		},
	},
}
