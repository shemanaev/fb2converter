/*
Package etree provides XML services through an Element Tree abstraction.

Forked from https://github.com/beevik/etree at commit 6d7c4e994fa577a54ad1ae652d59bd7875b93460 and modified.

Original package at the time did not support mixed-content XML - which I needed. However my modifications introduced
breaking change to the package API, so PR was declined (properly) by Brett Vickers (original author). Later Brett added
support for mixed-content to his code.

Original code is copyrighted by Brett Vickers under BSD-style license. I will keep it here with original license and
list of contributors for reference at my code most likely will never make into Brett's code.
*/
package etree
