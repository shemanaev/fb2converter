/*
Package mobi is responsible for producing MOBI/AZW3 formats using Kindlegen and modifying/optimizing the result as
requested by configuration. It also has logic for thumbnail and pageing information extraction.

Most of the knowledge of the MOBI/AZW3 internals comes from "KindleUnpack" - python based software to unpack Amazon / Kindlegen generated ebooks
licensed by respective authors under GPL v3. Despite obvious ineffectiveness I decided to repeat python code "ad verbum" for now as it is very time
consuming debuging Amazon issues and incompatibilities and old code seems to be working well for a very long time.
Visit KindleUnpack - https://github.com/kevinhendricks/KindleUnpack for any information.
*/
package mobi
