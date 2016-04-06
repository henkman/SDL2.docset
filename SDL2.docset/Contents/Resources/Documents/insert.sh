typeset -i index=1

sqlite3 -echo ../docSet.dsidx "delete from searchIndex"

for dir in cat clconst enum func macro struct Union;
do
	cd $dir

	for file in *.html
	do
		ln -sf "$dir/$file" "../$file"
		sql="insert into searchIndex (id, name, type, path) values ($index, \"${file%\.html}\", \"$dir\", \"$file\");"
		echo -e $sql"\n"
		sqlite3 -echo ../../docSet.dsidx "$sql"
		index=$index+1
	done

	cd ..
done

