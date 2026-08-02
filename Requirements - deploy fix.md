Fist big issue - I cant find a way to define custom tool. Descriptors is the way, but not for the user when using deploy tool.
Second big issue related to first: Subagent in Claude code do not have access to AskUserQuestions. I have custom tool - mcp server, but it is always overwritter nwhen I deploy CC agents. Check - .claude\agents\codebase-research.md.
We need to allow user to defien its mapping AskUserQuestion = user-feedback. Additionaly, in most harnesses ths would simpyl mean toi put user-feedback into tools list, but claude code uses extra mcp servers field instead.
I want to be able to configure this in harness descriptors - how is actually custom tool name mapped. And user must be able to configure mapping itself in his yamls. 
It should be possible to map tool into mutliple options, so AskUserQuestion produces both harness tool and mcp server.

Then there are other small issues.:
1. Something is wrong with detection model IDs. C:\AI\MOSAIC\MOSAIC\Tools\Deployment\descriptors\claude-code.yaml has plenty of models, but when I deploy CC, I have only 3 options - opus 4.6, sonet 4.6, haiku 4.6. descriptor field does nothing. I am sure I rebuild and redeployed correctly.
2. There is no easy way to update just the workflows in orchestrator. Most likely it should be third option when picking up deployment options. It must remvoe all non selected workflows from orchestrator, and inject selected ones. Rewrite/backup options for lcoal chanegs should be same as when updating agents. It can also update the orchestrator.. I thik  easiest way to do this is simply redeploy orchestrator only, with workflos selection

Still whole mosaic is under heavy development, nothing was released yet. Therefore there should be no backwards compatibility concerns at all.