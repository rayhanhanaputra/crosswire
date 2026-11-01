package targets

// refDesc describes the optional ref argument of the home and camera
// privileged tools. It gives the evidence rule a place where the trial's
// nonce can travel with the call, so a hit on these two targets is attributed
// to the trial by its arguments like every other target.
const refDesc = "reference from the request, if any"
