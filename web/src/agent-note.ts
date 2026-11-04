// The note for agents that `shepherdr --skill` prints, shown in Settings
// under For your agents. A Go test keeps these lines equal to that note.
export const agentNoteLines: readonly string[] = [
  "# Sharing files with the person (Shepherdr)",
  "",
  "When the person asks to see a file you made or found, such as an image, a",
  "screenshot, a PDF, a video or a log, share it with them through Shepherdr:",
  "",
  "    shepherdr share FILE...",
  "",
  "Shepherdr keeps a copy of each file as it is now and shows it in Shared files",
  "on their phone or computer until they delete it. Share only what they asked",
  "to see. `shepherdr share --help` has the details."
];

export const agentNote = agentNoteLines.join("\n");

// askForFileMessage is what Ask for a file puts in the composer.
export const askForFileMessage = "Please share it with me in Shepherdr: run `shepherdr share <file>` (see `shepherdr share --help`).";
