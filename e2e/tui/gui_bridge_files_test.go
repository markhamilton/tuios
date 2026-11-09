package tuie2e

// The explorer's half of the bridge (wave 5): a verb the renderer sends with
// a host runs on that machine's daemon, through this machine's daemon and its
// link, so the GUI lists, previews and copies another machine's files with
// the same verbs it uses here. A verb that answers for the person never goes
// to another machine with this client's nonce.
//
// What would pass a weaker test and fail this one: a bridge that ignores host
// (the listing is this machine's folder, or "does not exist"), and a bridge
// that sends the nonce along (respond on build would get one).
