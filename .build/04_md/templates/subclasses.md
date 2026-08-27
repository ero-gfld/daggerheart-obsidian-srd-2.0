# {{ .name }}

{{ .description }}
{{- if .spellcast_trait }}

### {{ headingOr .spellcast_heading "SPELLCAST TRAIT" }}

{{ .spellcast_trait }}
{{- end }}

### {{ literalHeading .foundation_heading "FOUNDATION FEATURE" (len .foundation) }}

{{- range .foundation }}

**_{{ featureLabel .name }}_** {{ .text }}
{{- end }}

### {{ literalHeading .specialization_heading "SPECIALIZATION FEATURE" (len .specialization) }}

{{- range .specialization }}

**_{{ featureLabel .name }}_** {{ .text }}
{{- end }}

### {{ literalHeading .mastery_heading "MASTERY FEATURE" (len .mastery) }}

{{- range .mastery }}

**_{{ featureLabel .name }}_** {{ .text }}
{{- end }}

{{- if eq .name "Beastbound" }}

### RANGER COMPANION

When you choose the Beastbound Ranger subclass, take a companion sheet. This sheet is for tracking important information about your character's companion and can be tucked beneath the right side of your character sheet for ease of viewing.

##### STEP 1: NAME YOUR COMPANION

Work with the GM to decide what kind of animal you have as your companion. Give them a name and add a picture of them to the companion sheet.

##### STEP 2: WRITE THEIR EVASION

Fill in their Evasion, which starts at 10.

##### STEP 3: CHOOSE THEIR COMPANION EXPERIENCE

Create two Experiences for your companion based on their training and the history you have together.

Start with +2 in both Experiences. Whenever you gain a new Experience, your companion also gains one. All new Experiences start at +2.

> **Example Companion Experiences**
>
> _Bold Distraction, Expert Climber, Fetch, Friendly, Guardian of the Forest, Horrifying, Intimidating, Loyal Until the End, Navigation, Nimble, Nobody Left Behind, On High Alert, Protective, Royal Companion, Scout, Service Animal, Trusted Mount, Vigilant, We Always Find Them, You Can't Hit What You Can't Find_

##### STEP 4: CHOOSE THEIR ATTACK AND RECORD DAMAGE DIE

Finally, describe your companion's method of dealing damage (their standard attack) and record it in the "Attack & Damage" section. At level 1, your companion's damage die is a d6 and their range is Melee. Choose whether they deal physical or magic damage.

#### WORKING WITH YOUR COMPANION

The following sections will run you through the basics of working with your companion.

##### USING SPELLCAST ROLLS, HOPE, AND EXPERIENCES

Make a Spellcast Roll to connect with your companion and command them to take action. Spend a Hope to add an applicable Companion Experience to the roll. On a success with Hope, if your next action builds on their success, you gain advantage on the roll.

##### ATTACKING WITH YOUR COMPANION

When you command your companion to attack, they gain any benefits that would normally only apply to you (such as the effects of "Ranger's Focus"). On a success, their damage roll uses your Proficiency and their damage die.

##### TAKING DAMAGE AS STRESS

- When your companion would take any amount of damage, they mark a Stress. When they mark their last Stress, they drop out of the scene (by hiding, fleeing, or a similar action). They remain unavailable until the start of your next long rest, where they return with 1 Stress cleared.
- When you choose a downtime move that clears Stress on yourself, your companion clears an equal number of Stress.

#### LEVELING UP YOUR COMPANION

When your character levels up, choose one available option for your companion from the following list and mark it on your sheet:

- **_Intelligent:_** Your companion gains a permanent +1 bonus to a Companion Experience of your choice.
- **_Light in the Dark:_** Use this as an additional Hope slot your character can mark.
- **_Creature Comfort:_** Once per rest, when you take time during a quiet moment to give your companion love and attention, you can gain a Hope or you can both clear a Stress.
- **_Armored:_** When your companion takes damage, you can **mark one of your Armor Slots** instead of marking one of their Stress.
- **_Vicious:_** Increase your companion's damage dice or range by one step (d6 to d8, Close to Far, etc.).
- **_Resilient:_** Your companion gains an additional Stress slot.
- **_Bonded:_** When you mark your last Hit Point, your companion rushes to your side to comfort you. Roll a number of **d6s** equal to the unmarked Stress slots they have and mark them. If any roll a 6, your companion helps you up. Clear your last Hit Point and return to the scene.
- **_Aware:_** Your companion gains a permanent +2 bonus to their Evasion.
  {{- end }}

{{- if eq .name "Martial Artist" }}

### MARTIAL STANCES

When you choose the Martial Artist subclass, take the Martial Stances sheet to track which stances your character knows. You can also track which stance you have active and your current Focus.

#### STANCES

Stances are special body positionings that enable Martial Artists to attack, react, or defend in combat. Each stance grants your character a different ability, bonus, or feature while active.

You choose two stances from Tier 1 when you first take the Martial Artist's subclass foundation card. Mark the circle next to each to indicate that your character knows and has access to them. Mark a new stance from your tier or below each time you gain a level.

#### FOCUS

Focus represents your character's poise, clarity, and control. Once per rest during a moment of calm, you can clear your mind and refocus your martial instincts. Clear your Focus track, then roll a number of **d6s** equal to your Instinct and gain Focus equal to the highest result rolled. You can hold a maximum of 6 Focus.

#### SHIFTING INTO STANCES

You can **spend a Focus** to shift into a martial stance and gain its effects. You can shift only into stances you've marked as known. When you shift into a stance, it's considered your active stance until you drop out of it.

You can't shift into or have more than one active stance at a time. Until you shift into a stance, you are not considered to be in a stance or to have an active stance.

#### DROPPING OUT OF STANCES

If you are already in a stance when you shift into a different stance, you automatically drop out of the previously active stance. You also drop out of your active stance when you take Severe damage or mark your last Hit Point. Otherwise, you drop out of your active stance at the end of the scene. When you drop out of a stance, you lose any ongoing benefits it provides.

#### STANCE FEATURES

The following section lists all martial stances by tier.

##### TIER 1

**_Favored:_** Gain a bonus to damage rolls equal to a trait of your choice.

**_Invigorating:_** On a successful attack, roll a **d4**. On a result of 4, gain a Focus.

**_Quick:_** When you make an attack, you can **spend a Focus** or **mark a Stress** to target another creature within range with that attack.

**_Reliable:_** Gain a +1 bonus to your attack rolls.

##### TIER 2

**_Aggressive:_** Gain a -1 penalty to your Evasion. On a successful attack, roll an additional damage die and discard the lowest result.

**_Anchored:_** Gain a +2 bonus to your damage thresholds. While in this stance, you can't be moved against your will.

**_Defensive:_** Attack rolls targeting you from within Melee range have disadvantage unless the attacker marks a Stress to negate the disadvantage.

**_Otherworldly:_** On a successful attack, you can deal physical or magic damage.

##### TIER 3

**_Grappling:_** On a successful attack within Melee range, you can **spend a Focus** or **mark a Stress** to temporarily _Restrain_ the target or throw the target up to Close range.

**_Scary:_** On a successful attack, the target must mark a Stress.

**_Stable:_** You can **spend a Focus** instead of an Armor Slot to reduce damage.

**_Vigilant:_** When you are targeted by an attack, you can **mark a Stress** to gain a **d6** bonus to your Evasion against the attack.

##### TIER 4

**_Crushing:_** When you deal Severe damage, you can **spend a Hope** to force the target to mark an additional Hit Point.

**_Exacting:_** When you roll a 1 on a damage die, you can treat it as the highest value on the die instead.

**_Honed:_** **Spend a Focus** before you make an attack roll to gain a +1 bonus to your Proficiency for that attack.

**_Isolating:_** Gain advantage on attack rolls when there are no other creatures within Very Close range of you or your target.
{{- end }}
