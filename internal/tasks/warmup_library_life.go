package tasks

// Hand-written threads for everyday topics; IDs and versions are assigned in warmupConversations.
var warmupLibraryLife = []Conversation{
	{
		Theme:       "reading",
		Subject:     "A novel for the train",
		Description: "Hi,\n\nI finally have a longer commute again and want something to read on the train. You always seem to have a novel going. Anything you have enjoyed lately that is gripping without being too heavy?",
		Messages: []string{
			"The one I keep recommending is a slow-burn mystery set in a small fishing town. The chapters are short, which is perfect for a commute, and the ending genuinely surprised me. I also liked a memoir by a lighthouse keeper, though it is much quieter.",
			"The fishing town one sounds right. Is it part of a series, or does it stand on its own?",
			"It stands on its own, so you can start without any homework. Enjoy the ride.\n\nTalk soon,",
		},
	},
	{
		Theme:       "podcasts",
		Subject:     "Podcasts for long drives",
		Description: "Got a long drive coming up and I am tired of my usual playlist. What podcasts are you into these days?",
		Messages: []string{
			"Mostly history ones. There is a show that takes one strange historical episode and spends a whole season on it, which makes the miles disappear. I also like an interview series with craftspeople talking about how they learned their trade.",
			"The craftspeople one is exactly my kind of thing. I will load a few episodes before I leave.",
			"Hope the drive goes smoothly. Happy listening.",
		},
	},
	{
		Theme:       "cooking",
		Subject:     "Tried that braised lentil recipe",
		Description: "I made the braised lentils you mentioned a while back. I used smoked paprika because I was out of cumin, and it worked surprisingly well. Leftovers were even better the next day. Thanks for the nudge to try it.",
		Messages: []string{
			"Smoked paprika is a smart swap. I usually add a squeeze of lemon right at the end, which brightens it up a lot. Did you serve it with anything?",
			"Just some crusty bread and a green salad. The lemon goes in next round.",
			"That sounds like the right combination. Glad it turned out well.",
		},
	},
	{
		Theme:       "hiking",
		Subject:     "Weekend trail suggestions",
		Description: "Hey,\n\nWe are thinking about a day hike this weekend, something moderate with a view at the top. You know the hills around here better than I do. Any trails you would point us toward?",
		Messages: []string{
			"The ridge loop on the north side is my favorite. It takes a good half day at an easy pace, with one steep stretch near the end, and the view over the valley is worth it. Go early, because the small parking area fills up fast.",
			"Perfect, the ridge loop it is. Is the trail well marked, or should we bring a map?",
			"It is marked the whole way, though a map never hurts on the descent. Have a great hike.",
		},
	},
	{
		Theme:       "running",
		Subject:     "Getting back into running",
		Description: "After a long break I have started running again, and my knees are not thrilled about it. You ran pretty consistently for a while, so I figured I would ask how you build up after time off without overdoing it.",
		Messages: []string{
			"Slowly, mostly. I did run and walk intervals for the first few weeks and only added distance once a run felt easy. Softer surfaces helped my knees too, so I stuck to gravel paths when I could.",
			"Run and walk intervals feel a little humbling, but that makes sense. I will give the gravel path by the river a go.",
			"Humbling is fine as long as it keeps you running. Good luck with it.\n\nCheers,",
		},
	},
	{
		Theme:       "travel",
		Subject:     "A few days on the coast",
		Description: "Hello,\n\nI have a few days off soon and I am heading to the coast for the first time. Since you go fairly often, I wanted to ask what is worth seeing and what I can skip. I am more into walking and good food than big attractions.\n\nThanks,",
		Messages: []string{
			"For walking, the cliff path between the two harbor towns is wonderful, especially in the late afternoon light. For food, skip the places right on the promenade and go one street back, where the small seafood kitchens are. The aquarium is fine but you can miss it.",
			"One street back, got it. Any particular dish I should not leave without trying?",
			"The fish stew, if you see it on a chalkboard menu. Have a lovely trip.",
		},
	},
	{
		Theme:       "gardening",
		Subject:     "Tomatoes are finally ripening",
		Description: "A short update from the garden: the tomatoes are finally turning red after weeks of green. The basil next to them has gone a bit wild, so I am making pesto this weekend.",
		Messages: []string{
			"That is great news. Mine split after the heavy rain, so I am a little jealous. Are you staking yours or letting them sprawl?",
			"Staking, after last season's tangle. It takes more time up front but picking is so much easier.",
			"Sounds like stakes are the answer. Enjoy the pesto.",
		},
	},
	{
		Theme:       "home office",
		Subject:     "Desk setup advice",
		Description: "Hello,\n\nI am redoing my desk at home and could use a second opinion. My neck has been sore by the end of the day, and I suspect the monitor is too low. What changes made the biggest difference for you?",
		Messages: []string{
			"Raising the monitor to eye level was the big one. I put mine on a sturdy box for a week before buying a proper stand, just to be sure. A chair with real lower back support was the other change I would not undo.\n\nBest,",
			"The box idea is clever. I can try that right away with a stack of books.",
			"Books work just as well. Hope your neck feels better soon.",
		},
	},
	{
		Theme:       "tea",
		Subject:     "Switching to tea",
		Description: "Lately I have been trying to cut back on coffee and have started drinking more tea in the afternoons. The trouble is that most of what I have tastes like hot water. You always have a good loose leaf on your desk, so I am curious what you would suggest for a beginner. Nothing too floral, if possible.",
		Messages: []string{
			"Try a roasted green tea. It is toasty and mellow, almost nutty, and very hard to get wrong. Brew it a little cooler than boiling and for less time than you think.",
			"Toasty and mellow sounds much more my speed. How long do you usually steep it?",
			"Around a minute is plenty, then taste and adjust from there. Enjoy the switch.",
		},
	},
	{
		Theme:       "language",
		Subject:     "Language practice ideas",
		Description: "Hey,\n\nI have been learning Portuguese for a few months with an app, and I can read a menu but go blank the moment someone speaks to me. How did you get past that stage when you were learning?",
		Messages: []string{
			"Conversation exchanges helped me most. I met someone online who wanted to practice English, and we split each session half and half. It was awkward for the first few and then suddenly it was not.",
			"Half and half is a nice way to make it feel fair. I think I have been hiding behind the app for too long.",
			"Everyone does at first. You will be surprised how quickly it comes together once you start talking.\n\nBest,",
		},
	},
	{
		Theme:       "photography",
		Subject:     "Learning manual mode",
		Description: "My camera is finally off the full auto setting, and everything I shoot is either too dark or completely blown out. Is there one setting you would focus on first?",
		Messages: []string{
			"Start with aperture priority. You choose how much of the scene is in focus and the camera handles the rest, so you only have one thing to think about. Once that feels natural, move on to full manual.",
			"That sounds much less overwhelming. I will spend the weekend with it and see how it goes.",
			"Shooting a lot is the quickest way to learn. Have fun with it.",
		},
	},
	{
		Theme:       "board games",
		Subject:     "Game night was a hit",
		Description: "Thanks again for suggesting that cooperative card game. We played it at our game night and even the people who usually sit out joined in. It was nice to have everyone working together instead of arguing over rules.",
		Messages: []string{
			"So glad it went over well. The cooperative ones are great for mixed groups. If they liked that, the tile-laying one with the little castles is a good next step.",
			"Noted, I will look for it before the next one. Thanks again.",
		},
	},
	{
		Theme:       "moving",
		Subject:     "Moving tips welcome",
		Description: "Hi,\n\nWe are moving at the end of the month and the packing has barely started. You moved not too long ago. Was there anything you wish you had done differently?",
		Messages: []string{
			"Pack one box with everything you need for the first night: kettle, chargers, sheets, a few tools. Label it clearly and keep it with you rather than in the van. And start with the rooms you use least.",
			"A first night box is a great idea. We will start on the spare room this weekend.",
			"That is the right place to start. Good luck with the move, and enjoy the new place.",
		},
	},
	{
		Theme:       "volunteering",
		Subject:     "Volunteering at the food bank",
		Description: "Spent a morning volunteering at the local food bank and came away really glad I went. The team there was so organized, and it felt good to do something with my hands for a change. I am planning to go back once a month.\n\nIf you ever feel like joining, they always need people for the sorting shifts.",
		Messages: []string{
			"That sounds like a rewarding way to spend a morning. I have been meaning to find something like that. Do they need any experience, or can someone just show up?",
			"No experience at all. They walk you through everything on your first shift, and the regulars are friendly.",
			"Good to know, and thanks for sharing it.",
		},
	},
	{
		Theme:       "documentary",
		Subject:     "That documentary about deep sea life",
		Description: "Watched a documentary about deep sea creatures last night and I cannot stop thinking about it. There is a fish that makes its own light to lure prey, and the footage looked like something from another planet. You would love it.",
		Messages: []string{
			"I am always up for anything about the ocean. Is it more science heavy or more about the visuals?",
			"A good balance. The narration explains just enough without slowing it down, and the camera work is stunning.",
			"Sold. Adding it to the list for a quiet evening.",
		},
	},
	{
		Theme:       "sleep",
		Subject:     "Better sleep routine",
		Description: "For a few weeks I have been sleeping badly, so I decided to try a stricter evening routine: no screens for the last hour, the same bedtime every night, and a cooler bedroom. Early days, but it seems to help.\n\nHave you ever tried anything like that?",
		Messages: []string{
			"I have, and the consistent bedtime made the biggest difference for me, even on weekends. Reading something on paper instead of scrolling helped too.",
			"The weekends are where I always slip. I will hold the line and see how it goes.",
			"It gets easier after the first couple of weeks. Sleep well.",
		},
	},
	{
		Theme:       "pottery",
		Subject:     "My first pottery class",
		Description: "Tried a pottery class this week. Everything I threw on the wheel collapsed except for one small, lopsided bowl, which I am weirdly proud of. Highly recommend it if you want something that keeps you off your phone for a couple of hours!",
		Messages: []string{
			"A lopsided bowl is still a bowl. I have always wanted to try the wheel. Is it hard to center the clay?",
			"Very. The instructor said it takes most people several sessions before it comes naturally, so I am not giving up yet.",
			"That is encouraging. Keep the bowl, it will be fun to look back on.",
		},
	},
	{
		Theme:       "music",
		Subject:     "Back to the piano",
		Description: "I dusted off the piano after years away and my fingers have forgotten most of what they knew. I am trying to practice a little every day instead of long sessions on weekends. You play regularly, so do you have a routine that keeps you consistent?",
		Messages: []string{
			"Short and daily beats long and occasional, so you are on the right track. I start with five minutes of scales, then work on one piece in small sections rather than playing it start to finish.",
			"Small sections is the part I keep skipping. I will give that a proper try.",
			"It feels slow but it adds up quickly. Enjoy getting back into it.\n\nTake care,",
		},
	},
	{
		Theme:       "market",
		Subject:     "The farmers market is back",
		Description: "Went to the farmers market this morning for the first time this season. The strawberries were excellent, and there is a new stall selling sourdough that ran out before I left. Worth a visit if you are in the area on the weekend.",
		Messages: []string{
			"Good to hear it is open again. I usually go for the honey stand. Did you see if the cheese stall is still there?",
			"It is, same spot near the fountain. They had a smoked one I had not seen before.",
			"Great, that settles my weekend plans. Thanks for the heads up.",
		},
	},
	{
		Theme:       "weather",
		Subject:     "First cold snap",
		Description: "The first real cold morning of the season arrived. I had to scrape the car windows and realized I have no idea where my gloves went over the summer.",
		Messages: []string{
			"Same here. I found my scarf stuffed in a coat pocket from last winter. At least the light has been beautiful in the mornings.",
			"It has. The frost on the fields was worth the cold fingers.",
			"Agreed. Stay warm out there.",
		},
	},
	{
		Theme:       "pets",
		Subject:     "Life with a rescue dog",
		Description: "We brought home a rescue dog a couple of weeks ago. He is sweet but chews everything, including one of my shoes. You have had dogs for years, so any advice on getting through this stage would be welcome.",
		Messages: []string{
			"Chewing is mostly boredom and teething at that age. A couple of sturdy chew toys he only gets during quiet time, plus a long walk before you leave the house, made a huge difference for ours. It does pass.",
			"A longer morning walk is easy enough to manage. The rest of my shoes are going on a high shelf in the meantime.",
			"Wise move. Give him a scratch behind the ears from me.",
		},
	},
	{
		Theme:       "decluttering",
		Subject:     "Clearing out the closet",
		Description: "Spent the weekend going through the hall closet and filled three bags for donation. I kept finding things I had moved twice without ever opening. It feels lighter already.",
		Messages: []string{
			"That is a satisfying kind of tired. What finally got you started?",
			"Honestly, I could not close the door anymore. Once I began, deciding got easier with every bag.",
			"That is usually how it goes. Enjoy the extra space.",
		},
	},
}
