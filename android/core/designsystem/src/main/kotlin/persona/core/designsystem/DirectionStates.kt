package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.sizeIn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import persona.core.contracts.CoreText
import persona.core.contracts.DirectionCounts
import persona.core.contracts.DirectionTaskState
import persona.core.designsystem.icons.PhIcons

// The direction tree's words and colours (#4352), one place for the drawer, the To-dos page and the Agents page: the
// hub decides a task's state, these only say it. Needs you takes the needs-you colour, Working the working one, Stuck and
// To verify the caution colour; the words always go with the colour (docs/design/DESIGN_SYSTEM.md §6.3).

/** The colours of a task's [state]. */
fun PersonaPalette.directionTone(state: DirectionTaskState): StatusColors = when (state) {
    DirectionTaskState.NeedsYou -> status(Status.NeedsYou)
    DirectionTaskState.Working -> status(Status.Working)
    DirectionTaskState.Stuck, DirectionTaskState.Review -> tone(Tone.Caution)
}

/** A task's state word ("Needs you", "To verify", "Stuck", "Doing") in the page's language. */
val DirectionTaskState.localizedLabel: String @Composable get() = CoreText.of(label).text()

/** The secretary group's name (书记官): the to-dos the main conversation or no current direction supervises. */
val secretaryName: String @Composable get() = CoreText.of(CoreText.Key.DirectionSecretary).text()

/** A task's state in words, in its colour, for the end of a task row. */
@Composable
fun JDirectionStateLabel(state: DirectionTaskState, modifier: Modifier = Modifier) {
    Text(state.localizedLabel, modifier, style = PersonaTheme.type.caption, color = PersonaTheme.palette.directionTone(state).text,
        maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis)
}

/**
 * A direction's two counts, "Doing N · Needs you N" (`counts.working`, `counts.needs_you` of the hub's tree), in quiet
 * caption text; the Needs you part takes the needs-you colour while it is above zero.
 */
@Composable
fun JDirectionCounts(counts: DirectionCounts, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val full = CoreText.of(CoreText.Key.DirectionCounts, counts.working, counts.needsYou).text()
    val needsWord = CoreText.of(CoreText.Key.DirectionNeedsYou).text()
    val needsYouColor = palette.status(Status.NeedsYou).text
    val text = remember(full, needsWord, counts.needsYou, needsYouColor) {
        // The Needs you part is the end of the words in every language: it is coloured from its word on.
        val at = if (counts.needsYou > 0) full.lastIndexOf(needsWord) else -1
        buildAnnotatedString {
            if (at < 0) append(full)
            else {
                append(full.substring(0, at))
                withStyle(SpanStyle(color = needsYouColor)) { append(full.substring(at)) }
            }
        }
    }
    Text(text, modifier, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1, softWrap = false,
        overflow = TextOverflow.Ellipsis)
}

/**
 * The caret that folds a tree row open or shut, a button of its own (48dp) beside the row's own tap: TalkBack hears
 * [label] (what it folds), whether it is open and what a tap does ([foldToggle]).
 */
@Composable
fun JDisclosureButton(open: Boolean, onToggle: () -> Unit, label: String, modifier: Modifier = Modifier) {
    Box(modifier.sizeIn(minWidth = Tokens.MinTouch.android, minHeight = Tokens.MinTouch.android).foldToggle(open, onToggle)
        .semantics { contentDescription = label }, contentAlignment = Alignment.Center) {
        JIcon(PhIcons.CaretRight, null, size = Tokens.Space.l, tint = PersonaTheme.palette.ink3,
            modifier = Modifier.rotate(if (open) 90f else 0f), decorative = true)
    }
}
