# Gate all source cleanup behind an unchanged verified snapshot

djpi will not delete media or format storage merely because transfers returned
success. Cleanup becomes eligible only after every item in a complete Source
Snapshot has a content-verified Delivery at every required Destination and a
fresh snapshot proves the Source has not changed. The actual cleanup mechanism
remains camera-specific because current research proves per-file deletion but
does not prove a remote whole-card format command.

