package extshared

import "time"

// SendReplyMax follows ruling supplement59; leaves room under the default60s client deadline.
const SendReplyMax = 55 * time.Second
